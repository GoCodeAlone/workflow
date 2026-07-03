# wfctl Installation And Plugin Lifecycle

`wfctl` is published as raw release binaries, a GoCodeAlone Homebrew formula,
and a Go source install. Use Homebrew when you want package-manager upgrades.
Use the release binaries when you want a direct install without Homebrew.

## Install wfctl

### Homebrew

```bash
brew tap gocodealone/tap
brew install wfctl
wfctl --version
```

Upgrade with Homebrew:

```bash
brew update
brew upgrade wfctl
```

If a Workflow release was just published and Homebrew still shows an older
formula, run `brew update` before checking again:

```bash
brew info gocodealone/tap/wfctl
```

### Terminal Download Without Homebrew

Workflow releases publish raw `wfctl` binaries plus `checksums.txt`.
This installer downloads the latest binary for macOS or Linux, verifies the
SHA-256 checksum, and installs it into `/usr/local/bin`.

```bash
set -euo pipefail

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin|linux) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

asset="wfctl-${os}-${arch}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

base="https://github.com/GoCodeAlone/workflow/releases/latest/download"
curl -fL "$base/$asset" -o "$tmpdir/$asset"
curl -fL "$base/checksums.txt" -o "$tmpdir/checksums.txt"

(cd "$tmpdir" && grep "  $asset$" checksums.txt | shasum -a 256 -c -)
chmod +x "$tmpdir/$asset"
sudo install -m 0755 "$tmpdir/$asset" /usr/local/bin/wfctl

wfctl --version
```

For a user-local install without `sudo`, replace the install command with:

```bash
mkdir -p "$HOME/.local/bin"
install -m 0755 "$tmpdir/$asset" "$HOME/.local/bin/wfctl"
```

Then ensure `$HOME/.local/bin` is in `PATH`.

### Browser Download

1. Open <https://github.com/GoCodeAlone/workflow/releases/latest>.
2. Download the asset for your platform:
   - macOS Apple Silicon: `wfctl-darwin-arm64`
   - macOS Intel: `wfctl-darwin-amd64`
   - Linux x86_64: `wfctl-linux-amd64`
   - Linux ARM64: `wfctl-linux-arm64`
   - Windows x86_64: `wfctl-windows-amd64.exe`
3. Download `checksums.txt` from the same release.
4. Verify the checksum.

macOS or Linux:

```bash
shasum -a 256 --ignore-missing -c checksums.txt
chmod +x ./wfctl-*
sudo install -m 0755 ./wfctl-darwin-arm64 /usr/local/bin/wfctl
```

Replace `wfctl-darwin-arm64` with the file you downloaded.

Windows PowerShell:

```powershell
certutil -hashfile .\wfctl-windows-amd64.exe SHA256
```

Compare the printed hash to the `wfctl-windows-amd64.exe` row in
`checksums.txt`, then place the executable in a directory on `%PATH%`.

### From Source

Use this path when you already have Go installed and want a source build:

```bash
go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest
```

This installs into `$(go env GOPATH)/bin` or `$(go env GOBIN)`. Ensure that
directory is in `PATH`.

## Update wfctl

Homebrew installs should be updated by Homebrew:

```bash
brew update
brew upgrade wfctl
```

Release-binary installs can self-update:

```bash
wfctl update --check
wfctl update
```

`wfctl update` downloads the latest GitHub release for the current platform,
verifies `checksums.txt` when available, and replaces the current executable.
It is not recommended for Homebrew-managed binaries because it bypasses
Homebrew's cellar and formula state.

## Project Plugin Lifecycle

Workflow projects should commit a human-edited `wfctl.yaml` manifest and the
generated `.wfctl-lock.yaml` lockfile. The manifest states intent; the lockfile
pins resolved versions and portable archive checksums for CI.

```bash
# Find a plugin.
wfctl plugin search auth

# Add a plugin to wfctl.yaml and regenerate .wfctl-lock.yaml.
wfctl plugin add workflow-plugin-auth@v0.4.0

# Refresh the lockfile after editing wfctl.yaml.
wfctl plugin lock

# Install all locked project plugins into ./data/plugins.
wfctl plugin install

# CI path: install exactly from the lockfile and do not write files.
wfctl plugin ci
```

`wfctl plugin install` without a plugin argument checks `wfctl.yaml` and
`.wfctl-lock.yaml`. If the lockfile is missing or stale, local installs
regenerate it before installing. Use `wfctl plugin ci` or
`wfctl plugin install --locked` in CI to fail on stale locks instead of
modifying the checkout.

By default, project plugins install to `data/plugins`. Override the directory
with `--plugin-dir` or `WFCTL_PLUGIN_DIR`.

## Global Plugin Lifecycle

Global plugins are operator tools available outside a single project. They do
not modify `wfctl.yaml` or `.wfctl-lock.yaml`.

```bash
wfctl plugin install -g workflow-plugin-portfolio
wfctl plugin list -g
wfctl plugin info -g workflow-plugin-portfolio
wfctl plugin update -g workflow-plugin-portfolio
wfctl plugin update -g --all
wfctl plugin remove -g workflow-plugin-portfolio
```

The default global directory is
`${XDG_DATA_HOME:-$HOME/.local/share}/wfctl/plugins`. Override it with
`WFCTL_GLOBAL_PLUGIN_DIR`.

Project-local plugin commands take precedence over global plugin commands, so a
repository can pin the plugin version it expects while operators keep global
tools installed for ad hoc use.

## Plugin Integrity And Compatibility

Registry installs fail closed when `wfctl` cannot verify plugin archive
checksums. GitHub release downloads can auto-fetch `checksums.txt`; registry
manifests can also carry SHA-256 values directly. Use `--skip-checksum` only
for trusted internal URLs.

Registry installs and locks also resolve compatibility evidence for the current
Workflow engine version. Use `--compat-mode warn` to allow warnings during
migration, or `--force` only when you intentionally accept known-failing or
missing compatibility evidence while keeping checksum enforcement.

## Useful References

- Full CLI reference: [WFCTL.md](WFCTL.md)
- Plugin authoring: [PLUGIN_DEVELOPMENT_GUIDE.md](PLUGIN_DEVELOPMENT_GUIDE.md)
- Registry: <https://github.com/GoCodeAlone/workflow-registry>
- Releases: <https://github.com/GoCodeAlone/workflow/releases>
