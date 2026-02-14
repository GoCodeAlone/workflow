# Multi-stage build for the Workflow e-commerce example
# Build:   docker build -t workflow-store -f example/ecommerce-app/Dockerfile .
# Run:     docker run -p 8080:8080 -e JWT_SECRET=your-secret workflow-store

FROM dhi.io/golang:1.26-alpine3.22-dev AS builder

WORKDIR /build

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Build the server binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# --- Runtime stage (distroless-style minimal image) ---
FROM dhi.io/static:20250911-alpine3.22

WORKDIR /app

# Copy binary
COPY --from=builder /build/server .

# Copy example config, seed data, SPA assets, and dynamic components
COPY example/ecommerce-app/workflow.yaml   ./config/workflow.yaml
COPY example/ecommerce-app/configs/        ./config/configs/
COPY example/ecommerce-app/seed/           ./config/seed/
COPY example/ecommerce-app/spa/            ./config/spa/
COPY example/ecommerce-app/components/     ./config/components/

# Run as nonroot user (UID 65532, pre-configured in dhi.io/static)
USER nonroot

EXPOSE 8080

CMD ["./server", "-config", "config/workflow.yaml"]
