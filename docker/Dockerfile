# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=0.1.0 -X main.commit=docker" -o /out/mdcix ./cmd/mdcix

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/mdcix /usr/local/bin/mdcix
EXPOSE 8080
VOLUME ["/certs"]
ENTRYPOINT ["mdcix"]
CMD ["serve", "--listen", ":8080", "--cert-dir", "/certs"]
