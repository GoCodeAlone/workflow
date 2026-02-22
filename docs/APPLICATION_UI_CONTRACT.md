# Application UI Build/Serve Contract

This document defines the conventions and contracts for building and serving
application UIs in the workflow engine ecosystem.

## Overview

Workflow applications can bundle a React (or any Node.js-based) UI alongside
their backend configuration. The engine serves the built static assets through
the `static.fileserver` module. No custom server code is required.

The contract has two parts:

1. **Build contract** — how to produce a deployable UI artifact
2. **Serve contract** — how to configure the engine to serve it

---

## Build Contract

### Directory layout

By convention, place the UI source under a `ui/` directory in your project root:

```
my-app/
  ui/                    # UI source
    src/
    index.html
    package.json
    vite.config.ts
    dist/                # Build output (git-ignored)
      index.html
      assets/
        index-abc123.js
        index-abc123.css
  config.yaml            # Workflow engine config
```

### Build output requirements

A valid build **must** produce:

| Path | Requirement |
|------|-------------|
| `ui/dist/index.html` | Entry point HTML file |
| `ui/dist/assets/*.js` | At least one JavaScript bundle |
| `ui/dist/assets/*.css` | At least one CSS stylesheet |

The `wfctl build-ui --validate` command checks these requirements without
running a build, which is useful in CI to verify artifacts.

### Building with `wfctl build-ui`

```bash
# Build with defaults (detects ./ui, runs npm ci + npm run build, validates)
wfctl build-ui

# Specify a non-standard UI directory
wfctl build-ui --ui-dir ./frontend

# Build and copy output to the module embed directory
wfctl build-ui --output ./module/ui_dist

# Only validate an existing build (no npm commands)
wfctl build-ui --validate

# Print the YAML config snippet for serving the UI
wfctl build-ui --config-snippet
```

**What `wfctl build-ui` does:**

1. Detects the UI framework (Vite, Next.js, Angular, plain Node)
2. Runs `npm ci` (if `package-lock.json` exists) or `npm install`
3. Runs `npm run build`
4. Validates that `dist/index.html`, `dist/assets/*.js`, and `dist/assets/*.css` exist
5. Optionally copies `dist/` contents to `--output`
6. Optionally prints the static.fileserver YAML config

### Framework detection

`wfctl build-ui` auto-detects the framework by checking for config files:

| File | Detected framework |
|------|--------------------|
| `vite.config.ts` / `vite.config.js` | vite |
| `next.config.ts` / `next.config.js` | next |
| `angular.json` | angular |
| `package.json` only | node (generic) |

The build command is always `npm run build`. This matches the convention that
all supported frameworks expose their build step through this script.

### Embedding UI assets in the Go binary

If you want to embed the UI in the binary (no external files at runtime), copy
`dist/` into a directory covered by a `//go:embed` directive before building:

```bash
# Copy built UI into the embed path
wfctl build-ui --output ./module/ui_dist

# Then build the Go binary (go:embed reads from module/ui_dist/)
go build -o server ./cmd/server
```

---

## Serve Contract

### `static.fileserver` module

The `static.fileserver` module serves static files and supports SPA routing.
Add it to your `config.yaml` modules list:

```yaml
modules:
  - name: "app-ui"
    type: "static.fileserver"
    config:
      root: "./ui/dist"          # Path to the built UI directory
      prefix: "/"                # URL prefix to serve files under
      spaFallback: true          # Route unknown paths to index.html (SPA support)
      cacheMaxAge: 3600          # Cache-Control max-age in seconds (default: 0)
```

### All config options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `root` | string | required | Filesystem path to serve files from |
| `prefix` | string | `"/"` | URL prefix. Files at `{root}/file.js` are served at `{prefix}/file.js` |
| `spaFallback` | bool | `false` | Serve `index.html` for all paths that don't match a file |
| `cacheMaxAge` | int | `0` | `Cache-Control: max-age` value in seconds |
| `gzipEnabled` | bool | `false` | Serve pre-compressed `.gz` files when the client accepts gzip |
| `dependsOn` | []string | `[]` | Module names that must start before this module |

### SPA routing

Enable `spaFallback: true` for single-page applications that use client-side
routing (React Router, Vue Router, etc.). Without it, direct navigation to a
URL like `/dashboard` returns 404 because `dashboard` is not a file in `dist/`.

With `spaFallback: true`:
- Requests for existing files (`/assets/index.js`) are served normally
- All other requests are served `index.html`, letting the SPA router handle them

---

## Development Workflow

During development, run the Vite dev server instead of the built files. It
provides hot module replacement (HMR) so changes appear instantly.

```bash
# Start the backend (workflow engine)
wfctl run config.yaml

# In a separate terminal, start the Vite dev server
cd ui && npm run dev   # default port: 5173
```

### Proxy API requests in development

Configure Vite to proxy backend API calls so both run on different ports without
CORS issues. In `ui/vite.config.ts`:

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
})
```

With this config, `fetch('/api/v1/modules')` in the UI is proxied to the
workflow engine on port 8080 during development, and served by the engine
itself in production (since the UI is bundled into the same server).

---

## Production Deployment

### Standalone binary

```bash
# Build UI and copy into embed directory
wfctl build-ui --output ./module/ui_dist

# Build the binary (embeds ui_dist/ via go:embed)
go build -o myapp ./cmd/server

# Run
./myapp -config config.yaml
```

### Docker multi-stage build

```dockerfile
# Stage 1: Build UI
FROM node:20-alpine AS ui-builder
WORKDIR /app/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
COPY --from=ui-builder /app/ui/dist ./module/ui_dist
COPY . .
RUN go build -o server ./cmd/server

# Stage 3: Runtime image
FROM alpine:3.20
WORKDIR /app
COPY --from=go-builder /app/server .
COPY config.yaml .
EXPOSE 8080
CMD ["./server", "-config", "config.yaml"]
```

### Docker Compose with external UI assets

If you prefer not to embed the UI, mount the `dist/` directory as a volume and
set `root` to the mounted path:

```yaml
# docker-compose.yaml
services:
  app:
    image: myapp:latest
    volumes:
      - ./ui/dist:/app/ui/dist:ro
    environment:
      - CONFIG_PATH=/app/config.yaml
```

```yaml
# config.yaml
modules:
  - name: "app-ui"
    type: "static.fileserver"
    config:
      root: "/app/ui/dist"
      spaFallback: true
```

---

## Examples

### Basic SPA

```yaml
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"

  - name: app-ui
    type: static.fileserver
    config:
      root: "./ui/dist"
      prefix: "/"
      spaFallback: true
      cacheMaxAge: 3600
```

### UI served under a path prefix

Serve the UI at `/app/` while keeping `/api/` for the backend:

```yaml
modules:
  - name: app-ui
    type: static.fileserver
    config:
      root: "./ui/dist"
      prefix: "/app/"
      spaFallback: true
```

### With authentication middleware

```yaml
modules:
  - name: auth
    type: http.middleware.jwt
    config:
      secret: "${JWT_SECRET}"
      excludePaths:
        - "/app/login"
        - "/app/assets/"

  - name: app-ui
    type: static.fileserver
    config:
      root: "./ui/dist"
      prefix: "/app/"
      spaFallback: true
      dependsOn:
        - auth
```

### With aggressive caching and gzip

```yaml
modules:
  - name: app-ui
    type: static.fileserver
    config:
      root: "./ui/dist"
      prefix: "/"
      spaFallback: true
      cacheMaxAge: 86400      # 24 hours — safe since Vite hashes asset filenames
      gzipEnabled: true       # Serve pre-compressed .gz files
```
