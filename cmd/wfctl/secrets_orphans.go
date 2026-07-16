package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

var (
	credentialOrphanOperationTimeout   = 10 * time.Minute
	paginateSpacesKeysByNameForOrphans = paginateSpacesKeysByName
	deleteSpacesKeyForOrphans          = deleteSpacesKey
)

// runSecretsListOrphans implements `wfctl secrets list-orphans`.
//
// Lists upstream provider credentials that share a name (typically orphans
// from past partial-apply failures). Installed credential-issuer plugins are
// selected by exact source; the legacy DigitalOcean path remains temporarily
// as a compatibility fallback.
//
// Use --delete to delete every match. Without --delete this is a
// dry-run (the default).
func runSecretsListOrphans(args []string) error {
	fs := flag.NewFlagSet("secrets list-orphans", flag.ContinueOnError)
	source := fs.String("source", "digitalocean.spaces", "Credential source (for example, digitalocean.spaces)")
	name := fs.String("name", "", "Filter by exact credential name (required)")
	doDelete := fs.Bool("delete", false, "Delete matching orphans (default: dry-run)")
	pluginDir := fs.String("plugin-dir", "", "Directory containing installed provider plugins (default: $WFCTL_PLUGIN_DIR or data/plugins)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required (the credential name to clean up; e.g. \"multisite-deploy-key\")")
	}

	ctx, stopProviderCommand := boundedProviderCommandContext(credentialOrphanOperationTimeout)
	defer stopProviderCommand()
	client, closePlugin, declaration, _, _, found, err := resolveCredentialIssuerCapability(ctx, *pluginDir, *source)
	if err != nil {
		return err
	}
	if found {
		if closePlugin != nil {
			defer closePlugin()
		}
		return listCredentialIssuerOrphans(ctx, client, *source, *name, *doDelete, credentialIdentifierSensitivity(declaration))
	}
	switch *source {
	case "digitalocean.spaces":
		return listSpacesOrphans(ctx, *name, *doDelete)
	default:
		return providerCapabilityNotFoundError{family: "credential source", key: *source}
	}
}

func listCredentialIssuerOrphans(ctx context.Context, client pb.CredentialIssuerClient, source, name string, deleteMatches, identifierSensitive bool) error {
	var records []*pb.CredentialRecord
	identifiers := make(map[string]struct{})
	pageToken := ""
	complete := false
	for page := 0; page < 100; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		response, err := client.List(ctx, &pb.CredentialListRequest{
			Source: source, Selector: &pb.CredentialSelector{LogicalName: name}, PageToken: pageToken, PageSize: 100,
		})
		if err != nil {
			return providerCapabilityTransportError("CredentialIssuer.ListOrphans", err)
		}
		if response.GetError() != nil {
			return fmt.Errorf("list credential orphans %s; provider error text suppressed", response.GetError().GetCode())
		}
		for _, record := range response.GetCredentials() {
			if record == nil || record.GetIdentifier() == "" || record.GetLogicalName() != name {
				return fmt.Errorf("list credential orphans returned a selector mismatch; refusing cleanup")
			}
			if _, duplicate := identifiers[record.GetIdentifier()]; duplicate {
				return fmt.Errorf("list credential orphans returned duplicate identifiers; refusing cleanup")
			}
			identifiers[record.GetIdentifier()] = struct{}{}
			records = append(records, record)
		}
		pageToken = response.GetNextPageToken()
		if pageToken == "" {
			complete = true
			break
		}
	}
	if !complete {
		return fmt.Errorf("list credential orphans exceeded page limit; refusing partial cleanup")
	}
	if len(records) == 0 {
		fmt.Printf("no orphans found for name=%q\n", name)
		return nil
	}

	fmt.Printf("found %d orphan(s) named %q:\n", len(records), name)
	for _, record := range records {
		if identifierSensitive || record.GetIdentifierSensitive() {
			fmt.Println("  identifier=[sensitive identifier redacted]")
		} else {
			fmt.Printf("  identifier=%s\n", record.GetIdentifier())
		}
	}
	if !deleteMatches {
		fmt.Println("\ndry-run; re-run with --delete to remove these orphans")
		return nil
	}

	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		operationID := credentialOrphanDeleteOperationID(source, name, record.GetIdentifier())
		response, err := client.Delete(ctx, &pb.CredentialDeleteRequest{OperationId: operationID, Source: source, Identifier: record.GetIdentifier()})
		if err != nil {
			return providerCapabilityTransportError("CredentialIssuer.DeleteOrphan", err)
		}
		if response.GetError() != nil {
			return fmt.Errorf("delete credential orphan %s; provider error text suppressed", response.GetError().GetCode())
		}
		if response.GetReconciliationState() != pb.CredentialReconciliationState_CREDENTIAL_RECONCILIATION_STATE_CONFIRMED || response.GetIdentifier() != record.GetIdentifier() {
			return fmt.Errorf("delete credential orphan returned an uncertain or mismatched acknowledgement; automatic retry blocked")
		}
	}
	fmt.Printf("\ndeleted %d/%d orphans\n", len(records), len(records))
	return nil
}

func credentialOrphanDeleteOperationID(source, logicalName, identifier string) string {
	digest := sha256.Sum256([]byte("wfctl-credential-orphan-delete-v1\x00" + source + "\x00" + logicalName + "\x00" + identifier))
	return fmt.Sprintf("%x", digest[:16])
}

// listSpacesOrphans paginates DO Spaces keys, prints every match by
// name, and (when del=true) deletes each one.
//
// The DO Spaces Keys API does NOT enforce name uniqueness, so a single
// name may map to many access_keys after a partial-apply bug. Each
// orphan is independently deletable by access_key.
func listSpacesOrphans(ctx context.Context, name string, del bool) error {
	token := os.Getenv("DIGITALOCEAN_TOKEN")
	if token == "" {
		return fmt.Errorf("DIGITALOCEAN_TOKEN not set")
	}

	matches, err := paginateSpacesKeysByNameForOrphans(ctx, token, name)
	if err != nil {
		return err
	}

	if len(matches) == 0 {
		fmt.Printf("no orphans found for name=%q\n", name)
		return nil
	}

	fmt.Printf("found %d orphan(s) named %q:\n", len(matches), name)
	for _, ak := range matches {
		fmt.Printf("  access_key=%s\n", ak)
	}

	if !del {
		fmt.Println("\ndry-run; re-run with --delete to remove these orphans")
		return nil
	}

	fmt.Println()
	deleted := 0
	for _, ak := range matches {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deleteSpacesKeyForOrphans(ctx, token, ak); err != nil {
			fmt.Fprintf(os.Stderr, "  delete %s: %v\n", ak, err)
			continue
		}
		fmt.Printf("  deleted %s\n", ak)
		deleted++
	}
	fmt.Printf("\ndeleted %d/%d orphans\n", deleted, len(matches))
	if deleted < len(matches) {
		return fmt.Errorf("delete incomplete (%d failures)", len(matches)-deleted)
	}
	return nil
}

// paginateSpacesKeysByName returns every access_key whose name field
// equals the requested name. Bounded at 200 pages × 200 keys = 40000.
//
// DO's Spaces Keys list endpoint allows per_page up to 200. Earlier
// 100-cap was leaving callers with partial results on accounts that
// have many same-named orphans. We also follow the absolute URL
// returned in `links.pages.next` rather than incrementing page locally
// — DO's pagination contract is that `next` is authoritative.
func paginateSpacesKeysByName(ctx context.Context, token, name string) ([]string, error) {
	var matches []string
	next := "https://api.digitalocean.com/v2/spaces/keys?per_page=200&page=1"
	pages := 0
	totalKeysSeen := 0
	for next != "" && pages < 200 {
		pages++
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("DO list spaces keys: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("DO list spaces keys: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var list struct {
			Keys []struct {
				Name      string `json:"name"`
				AccessKey string `json:"access_key"`
			} `json:"keys"`
			Links struct {
				Pages struct {
					Next string `json:"next"`
				} `json:"pages"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, fmt.Errorf("DO list spaces keys parse: %w", err)
		}
		totalKeysSeen += len(list.Keys)
		for _, k := range list.Keys {
			if k.Name == name {
				matches = append(matches, k.AccessKey)
			}
		}
		next = list.Links.Pages.Next
	}
	fmt.Fprintf(os.Stderr, "list-orphans: scanned %d page(s), %d total key(s), %d matches for name=%q\n", pages, totalKeysSeen, len(matches), name)
	return matches, nil
}

// deleteSpacesKey deletes one DO Spaces key by access_key.
func deleteSpacesKey(ctx context.Context, token, accessKey string) error {
	url := "https://api.digitalocean.com/v2/spaces/keys/" + accessKey
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
