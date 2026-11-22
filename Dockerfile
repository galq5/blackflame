# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24 AS build

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath \
    -ldflags "-s -w \
      -X github.com/galq/blackflame/internal/version.Version=$VERSION \
      -X github.com/galq/blackflame/internal/version.Commit=$COMMIT \
      -X github.com/galq/blackflame/internal/version.BuildDate=$BUILD_DATE" \
    -o /out/blackflame ./cmd/blackflame

FROM alpine:3.24.3

RUN addgroup -S -g 1000 blackflame \
    && adduser -S -D -H -u 1000 -G blackflame -h /data blackflame \
    && mkdir -p /data \
    && chown blackflame:blackflame /data

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/blackflame /usr/local/bin/blackflame

USER blackflame
WORKDIR /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/blackflame", "healthcheck", "--url", "http://127.0.0.1:8080/healthz"]

ENTRYPOINT ["/usr/local/bin/blackflame"]
CMD ["serve", "--mode", "ui", "--data", "/data", "--listen", ":8080"]
