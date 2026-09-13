# Minimal image for a self-hosted 1 vCPU / 512 MB–1 GB box.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/osh ./cmd/osh

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget \
    && adduser -D -H -u 1000 osh \
    && mkdir -p /data \
    && chown osh:osh /data
WORKDIR /app
COPY --from=build /out/osh /usr/local/bin/osh
COPY configs/config.example.json /app/config.json
EXPOSE 8080
ENV OSH_CONFIG=/app/config.json \
    OSH_DATA_DIR=/data \
    OSH_LISTEN=:8080
VOLUME ["/data"]
USER osh
CMD ["osh"]
