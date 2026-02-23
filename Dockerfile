# Multi-stage build for the Workflow engine server.
#
# Build:   docker build --build-arg NPM_TOKEN=$(gh auth token) -t workflow .
# Run:     docker run -p 8080:8080 -p 8081:8081 workflow -config /etc/workflow/config.yaml
# Admin:   docker run -p 8080:8080 -p 8081:8081 -e JWT_SECRET=secret workflow -config /etc/workflow/config.yaml --admin
#
# NPM_TOKEN is required for @gocodealone scoped packages from GitHub Packages.

# --- Stage 1: Build the React admin UI ---
# Use BUILDPLATFORM so npm ci runs natively (UI assets are platform-independent).
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui-builder

ARG NPM_TOKEN
WORKDIR /build/ui

COPY ui/package.json ui/package-lock.json ui/.npmrc ./
RUN --mount=type=secret,id=npm_token \
    if [ -f /run/secrets/npm_token ]; then \
      echo "//npm.pkg.github.com/:_authToken=$(cat /run/secrets/npm_token)" >> .npmrc; \
    elif [ -n "$NPM_TOKEN" ]; then \
      echo "//npm.pkg.github.com/:_authToken=${NPM_TOKEN}" >> .npmrc; \
    fi && \
    npm ci --silent && \
    sed -i '/^\/\/npm.pkg.github.com\/:_authToken/d' .npmrc

COPY ui/ .
RUN npx vite build

# --- Stage 2: Build the Go binary ---
# Use BUILDPLATFORM so go mod download runs natively; cross-compile via TARGETOS/TARGETARCH.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS go-builder

ARG TARGETOS TARGETARCH

# GoCodeAlone forks (yaegi, go-plugin) may have moved tags; bypass proxy/sumdb.
ENV GOPRIVATE=github.com/GoCodeAlone/* \
    GONOSUMCHECK=github.com/GoCodeAlone/*

RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Copy built UI assets into the embed directory
COPY --from=ui-builder /build/ui/dist/ module/ui_dist/

# Cross-compile for the target platform (no QEMU needed)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o server ./cmd/server

# --- Stage 3: Runtime ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 65532 nonroot

WORKDIR /app

COPY --from=go-builder /build/server .

USER nonroot

EXPOSE 8080 8081

ENTRYPOINT ["./server"]
