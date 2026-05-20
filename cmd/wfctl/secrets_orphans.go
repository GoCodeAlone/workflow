package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// runSecretsListOrphans implements `wfctl secrets list-orphans`.
//
// Lists upstream provider credentials that share a name (typically
// orphans from past partial-apply failures). Supports digitalocean.spaces
// today; other sources can be added by extending listOrphans below.
//
// Use --delete to delete every match. Without --delete this is a
// dry-run (the default).
func runSecretsListOrphans(args []string) error {
	fs := flag.NewFlagSet("secrets list-orphans", flag.ContinueOnError)
	source := fs.String("source", "digitalocean.spaces", "Credential source (digitalocean.spaces)")
	name := fs.String("name", "", "Filter by exact credential name (required)")
	doDelete := fs.Bool("delete", false, "Delete matching orphans (default: dry-run)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("--name is required (the credential name to clean up; e.g. \"multisite-deploy-key\")")
	}

	ctx := context.Background()
	switch *source {
	case "digitalocean.spaces":
		return listSpacesOrphans(ctx, *name, *doDelete)
	default:
		return fmt.Errorf("source %q not supported (supported: digitalocean.spaces)", *source)
	}
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

	matches, err := paginateSpacesKeysByName(ctx, token, name)
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
		if err := deleteSpacesKey(ctx, token, ak); err != nil {
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
// equals the requested name. Bounded at 100 pages × 100 keys = 10000.
func paginateSpacesKeysByName(ctx context.Context, token, name string) ([]string, error) {
	var matches []string
	page := 1
	for page <= 100 {
		url := fmt.Sprintf("https://api.digitalocean.com/v2/spaces/keys?per_page=100&page=%d", page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		for _, k := range list.Keys {
			if k.Name == name {
				matches = append(matches, k.AccessKey)
			}
		}
		if list.Links.Pages.Next == "" {
			break
		}
		page++
	}
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
