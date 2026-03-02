FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o mytest2 .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/mytest2 .
COPY --from=builder /app/workflow.yaml .

EXPOSE 8080

ENTRYPOINT ["./mytest2"]
CMD ["-config", "workflow.yaml"]
