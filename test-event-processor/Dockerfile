FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o test-event-processor .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/test-event-processor .
COPY --from=builder /app/workflow.yaml .

EXPOSE 8080

ENTRYPOINT ["./test-event-processor"]
CMD ["-config", "workflow.yaml"]
