package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runPluginMarketplaceVerify scans a GitHub org for merged main-branch wfctl.yaml files
// that reference the given plugin. Reports whether the plugin's registry
// status should be "verified" (>=1 active pin) or "experimental" (no pins).
//
// Backed by `gh api` (the official GitHub CLI) so the subcommand inherits the
// operator's existing GitHub auth and rate-limit budget. No new auth surface.
func runPluginMarketplaceVerify(args []string) error {
	fs := flag.NewFlagSet("plugin verify", flag.ContinueOnError)
	org := fs.String("org", "GoCodeAlone", "GitHub org to scan")
	jsonOut := fs.Bool("json", false, "Output JSON instead of human-readable text")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin verify [options] <plugin-name>

Scan a GitHub org for merged main-branch wfctl.yaml files that pin the
plugin. Reports the suggested registry status:

  - "verified"     >=1 active pin in a main-branch wfctl.yaml
  - "experimental" no active pins

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}
	pluginName := fs.Arg(0)

	pins, err := searchOrgForPluginPins(context.Background(), *org, pluginName, ghAPICmd)
	if err != nil {
		return fmt.Errorf("search org: %w", err)
	}

	verdict := "experimental"
	if len(pins) > 0 {
		verdict = "verified"
	}

	if *jsonOut {
		report := map[string]any{
			"plugin":    pluginName,
			"org":       *org,
			"status":    verdict,
			"pin_count": len(pins),
			"pinned_in": pins,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	fmt.Printf("Plugin:  %s\n", pluginName)
	fmt.Printf("Org:     %s\n", *org)
	fmt.Printf("Pins:    %d\n", len(pins))
	fmt.Printf("Verdict: %s\n", verdict)
	if len(pins) > 0 {
		fmt.Println("Pinned in:")
		for _, p := range pins {
			fmt.Printf("  - %s\n", p)
		}
	}
	if verdict == "experimental" {
		fmt.Println("\nNo active main-branch pins found. Manifest status should be 'experimental'.")
	} else {
		fmt.Println("\nActive pins found. Manifest status should be 'verified'.")
	}
	return nil
}

// ghAPICmd is the indirection point so tests can inject a fake gh binary.
// Default is the real `gh api` CLI.
//
// #nosec G204 -- the binary is the fixed string "gh" and `endpoint` is
// constructed from a literal-prefix + URL-escaped query inside this
// package (urlQueryEscape sanitises). No user-controlled shell metachars
// flow into the subprocess.
var ghAPICmd = func(ctx context.Context, endpoint string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", "api", endpoint)
	return cmd.Output()
}

// searchOrgForPluginPins queries the GitHub code-search API for `name:
// <plugin>` occurrences inside wfctl.yaml files within the org. Returns a
// list of repo+path strings.
func searchOrgForPluginPins(ctx context.Context, org, plugin string, ghAPI func(context.Context, string) ([]byte, error)) ([]string, error) {
	query := fmt.Sprintf(`filename:wfctl.yaml org:%s "name: workflow-plugin-%s"`, org, plugin)
	endpoint := fmt.Sprintf("search/code?q=%s&per_page=100", urlQueryEscape(query))

	body, err := ghAPI(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("gh api search: %w", err)
	}

	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Path       string `json:"path"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	pins := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		pins = append(pins, fmt.Sprintf("%s/%s", item.Repository.FullName, item.Path))
	}
	return pins, nil
}

// urlQueryEscape minimal escape for the GitHub code-search query string.
// We rely on `gh api` to handle most encoding; only spaces, quotes, and
// colons need percent-escaping for the endpoint URL.
func urlQueryEscape(q string) string {
	r := strings.NewReplacer(
		" ", "%20",
		`"`, "%22",
		":", "%3A",
	)
	return r.Replace(q)
}
