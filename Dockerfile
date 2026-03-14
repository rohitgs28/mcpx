FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcpx ./cmd/mcpx

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/mcpx /usr/local/bin/mcpx
COPY mcpx.yaml /etc/mcpx/mcpx.yaml

EXPOSE 8080
ENTRYPOINT ["mcpx"]
CMD ["-c", "/etc/mcpx/mcpx.yaml"]
