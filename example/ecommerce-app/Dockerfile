# Multi-stage build for the Workflow e-commerce example
# Build:   docker build -t workflow-store -f example/ecommerce-app/Dockerfile .
# Run:     docker run -p 8080:8080 -e JWT_SECRET=your-secret workflow-store

FROM golang:1.25-alpine AS builder

WORKDIR /build

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Build the server binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# --- Runtime stage ---
FROM alpine:3.19

RUN apk add --no-cache ca-certificates \
    && adduser -D -u 1000 appuser

WORKDIR /app

# Copy binary
COPY --from=builder /build/server .

# Copy example config, seed data, and SPA assets
COPY example/ecommerce-app/workflow.yaml ./config/workflow.yaml
COPY example/ecommerce-app/seed/          ./config/seed/
COPY example/ecommerce-app/spa/           ./config/spa/

USER appuser

EXPOSE 8080

CMD ["./server", "-config", "config/workflow.yaml"]
