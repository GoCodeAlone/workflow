FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o test-plugin .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/test-plugin .
COPY --from=builder /app/workflow.yaml .

EXPOSE 8080

ENTRYPOINT ["./test-plugin"]
CMD ["-config", "workflow.yaml"]
