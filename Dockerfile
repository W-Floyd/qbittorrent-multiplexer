FROM golang:alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN \
    CGO_ENABLED=0 \
    GOOS=linux \
    go build -a -installsuffix cgo -o qbittorrent-multiplexer .

FROM alpine:latest

WORKDIR /root

# Copy the binary from the builder stage
COPY --from=builder /src/qbittorrent-multiplexer .

ENTRYPOINT ["/root/qbittorrent-multiplexer"]
