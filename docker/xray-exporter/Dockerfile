FROM golang:1.24-alpine AS build

ARG XRAY_EXPORTER_VERSION=v0.2.0
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN apk add --no-cache ca-certificates curl perl

WORKDIR /src

RUN curl -fsSL "https://github.com/compassvpn/xray-exporter/archive/refs/tags/${XRAY_EXPORTER_VERSION}.tar.gz" \
    -o /tmp/xray-exporter.tar.gz \
    && tar -xzf /tmp/xray-exporter.tar.gz --strip-components=1 -C /src

RUN perl -0777 -i -pe 's@(?m)^\s*// Skip per-user traffic metrics to control cardinality\R\s*// This prevents creating thousands of series for individual users\R\s*if p\[0\] == "user" \{\R\s*continue\R\s*\}\R\R@@g' /src/exporter.go \
    && perl -0777 -i -pe 's@p := strings\.Split\(s\.GetName\(\), ">>>"\)\R@p := strings.Split(s.GetName(), ">>>")\n\n\t\tif len(p) < 4 {\n\t\t\tcontinue\n\t\t}\n@' /src/exporter.go

RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath -ldflags='-s -w' -o /out/xray-exporter .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=build /out/xray-exporter /usr/local/bin/xray-exporter

ENTRYPOINT ["/usr/local/bin/xray-exporter"]
