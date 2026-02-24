# Multi-stage build for the Workflow engine server.
#
# Build:   docker build -t workflow .
# Run:     docker run -p 8080:8080 workflow -config /etc/workflow/config.yaml
#
# The admin UI is served by the external workflow-plugin-admin binary,
# which is loaded at runtime from data/plugins/.

# --- Stage 1: Build the Go binary ---
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

# Cross-compile for the target platform (no QEMU needed)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o server ./cmd/server

# --- Stage 2: Runtime ---
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 65532 nonroot

WORKDIR /app

COPY --from=go-builder /build/server .

USER nonroot

EXPOSE 8080 8081

ENTRYPOINT ["./server"]
