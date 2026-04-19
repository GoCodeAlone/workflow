package registrydo

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/GoCodeAlone/workflow/plugin/registry"
)

type doTag struct {
	Tag       string `json:"tag"`
	UpdatedAt string `json:"updated_at"`
}

func pruneTagsFromJSON(ctx registry.Context, token, registryPath string, raw []byte, keepLatest int) error {
	var tags []doTag
	if err := json.Unmarshal(raw, &tags); err != nil {
		return fmt.Errorf("parse doctl tags output: %w", err)
	}

	// Sort newest first (ISO 8601 lexicographic order is correct).
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].UpdatedAt > tags[j].UpdatedAt
	})

	kept := 0
	for _, t := range tags {
		if t.Tag == "latest" {
			continue
		}
		kept++
		if kept <= keepLatest {
			continue
		}
		deleteArgs := []string{"registry", "repository", "delete-tag",
			registryPath, t.Tag, "--force"}
		cmd := exec.CommandContext(ctx, "doctl", deleteArgs...) //nolint:gosec
		cmd.Env = append(os.Environ(), "DIGITALOCEAN_TOKEN="+token)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(ctx.Out(), "warn: failed to delete tag %s: %v\n%s", t.Tag, err, out)
		} else {
			fmt.Fprintf(ctx.Out(), "deleted tag %s\n", t.Tag)
		}
	}
	return nil
}
