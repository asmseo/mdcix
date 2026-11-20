# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=0.2.0 -X main.commit=docker" -o /out/mdcix ./cmd/mdcix

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
	&& adduser -D -H -u 10001 mdcix \
	&& mkdir -p /data /certs \
	&& chown -R mdcix:mdcix /data /certs
COPY --from=build /out/mdcix /usr/local/bin/mdcix
USER mdcix
EXPOSE 8080
VOLUME ["/data", "/certs"]
ENV MDCIX_CACHE_DIR=/data
ENTRYPOINT ["mdcix"]
# Single-container default: REST API with persistent SQLite cache.
# Override command for forward proxy: ["serve","--listen",":8080","--cert-dir","/certs"]
CMD ["serve", "--api", "--listen", ":8080", "--cache-dir", "/data", "--cert-dir", "/certs"]
