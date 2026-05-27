// Package admin hosts the host-side infra.admin module's UI assets +
// audit subsystem. The handler library lives in the sibling
// handler/ subpackage; the catalog lives in catalog/; the proto in
// proto/. This package itself exposes only the asset filesystem +
// audit writer surface the host module (workflow/module/infra_admin.go,
// T15) imports.
//
// Design: docs/plans/2026-05-27-infra-admin-dynamic-design.md
// Plan:   docs/plans/2026-05-27-infra-admin-dynamic.md (Tasks 13 + 14)
package admin

import "embed"

// AssetFS embeds the static UI pages + scripts + styles authored in
// T10-T12 under ui_dist/. The host module (T15) mounts this via
// http.FileServerFS at config.AssetPrefix so the admin dashboard
// iframe can load resources.html / resource.html / new.html.
//
// Per plan §Task 13. The glob covers the three file types the asset
// pages use (.html / .js / .css); future additions (icons, fonts)
// require both extending this glob AND updating
// TestAssetFS_ListsAllAndOnlyExpected so the test catches the change.
//
//go:embed ui_dist/*.html ui_dist/*.js ui_dist/*.css
var AssetFS embed.FS
