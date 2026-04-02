FROM alpine:3.20

ARG XRAY_EXPORTER_VERSION=v0.2.0
ARG TARGETARCH

RUN apk add --no-cache ca-certificates curl

RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) asset="xray-exporter-linux-amd64" ;; \
      arm64) asset="xray-exporter-linux-arm64" ;; \
      arm) asset="xray-exporter-linux-arm" ;; \
      *) echo "Unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/compassvpn/xray-exporter/releases/download/${XRAY_EXPORTER_VERSION}/${asset}" \
      -o /usr/local/bin/xray-exporter; \
    chmod +x /usr/local/bin/xray-exporter

ENTRYPOINT ["/usr/local/bin/xray-exporter"]
