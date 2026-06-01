package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/GoCodeAlone/workflow/interfaces"
)

var (
	ownersStdout        io.Writer = os.Stdout
	ownersStderr        io.Writer = os.Stderr
	ownersLoadProviders           = defaultCleanupLoadProviders
)

func runInfraOwners(args []string) error {
	fs := flag.NewFlagSet("infra owners", flag.ContinueOnError)
	fs.SetOutput(ownersStderr)

	var configFile string
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	var envName string
	fs.StringVar(&envName, "env", "", "Environment name for config and state resolution")
	owner := fs.String("owner", "", "Owner identity to list (required)")
	resourceType := fs.String("type", "", "Optional resource type filter, e.g. infra.container_service")
	var pluginDirFlag string
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *owner == "" {
		return errors.New("infra owners: --owner is required")
	}

	prevInfraPluginDir := currentInfraPluginDir
	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = prevInfraPluginDir }()

	ctx := context.Background()
	providers, closers, err := ownersLoadProviders(ctx, fs, configFile, envName)
	if err != nil {
		return fmt.Errorf("load providers: %w", err)
	}
	defer func() {
		for _, c := range closers {
			if c == nil {
				continue
			}
			if cerr := c.Close(); cerr != nil {
				fmt.Fprintf(ownersStderr, "warning: provider shutdown: %v\n", cerr)
			}
		}
	}()

	rows := make([]interfacesOwnerRow, 0)
	var totalErrs []error
	for _, p := range providers {
		ownership, ok := p.(interfaces.OwnershipProvider)
		if !ok {
			fmt.Fprintf(ownersStdout, "skipped %s: provider does not implement OwnershipProvider\n", p.Name())
			continue
		}
		got, listErr := ownership.ListOwners(ctx, interfaces.OwnerFilter{Owner: *owner, ResourceType: *resourceType})
		if listErr != nil {
			if errors.Is(listErr, interfaces.ErrProviderMethodUnimplemented) {
				fmt.Fprintf(ownersStdout, "skipped %s: provider does not implement OwnershipProvider\n", p.Name())
				continue
			}
			totalErrs = append(totalErrs, fmt.Errorf("%s: list owners: %w", p.Name(), listErr))
			fmt.Fprintf(ownersStderr, "%s: list owners: %v\n", p.Name(), listErr)
			continue
		}
		rows = appendOwnerRows(rows, p.Name(), got)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Provider != rows[j].Provider {
			return rows[i].Provider < rows[j].Provider
		}
		if rows[i].Type != rows[j].Type {
			return rows[i].Type < rows[j].Type
		}
		return rows[i].Name < rows[j].Name
	})
	if len(rows) == 0 {
		fmt.Fprintf(ownersStdout, "no resources owned by %q\n", *owner)
	} else {
		fmt.Fprintln(ownersStdout, "PROVIDER\tTYPE\tNAME\tPROVIDER_ID\tOWNER\tSOURCE")
		for _, row := range rows {
			fmt.Fprintf(ownersStdout, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Provider, row.Type, row.Name, row.ProviderID, row.Owner, row.Source)
		}
	}
	if len(totalErrs) > 0 {
		return errors.Join(totalErrs...)
	}
	return nil
}

type interfacesOwnerRow struct {
	Provider   string
	Type       string
	Name       string
	ProviderID string
	Owner      string
	Source     string
}

func appendOwnerRows(rows []interfacesOwnerRow, provider string, owners []interfaces.ResourceOwner) []interfacesOwnerRow {
	for _, owner := range owners {
		rows = append(rows, interfacesOwnerRow{
			Provider:   provider,
			Type:       owner.Ref.Type,
			Name:       owner.Ref.Name,
			ProviderID: owner.Ref.ProviderID,
			Owner:      owner.Owner,
			Source:     owner.Source,
		})
	}
	return rows
}
