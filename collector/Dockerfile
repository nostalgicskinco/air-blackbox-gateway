# Build a custom OTel Collector with AIR processors baked in.
#
# Uses OCB (OpenTelemetry Collector Builder) to compile a single binary
# that includes genai_semantic_normalizer, promptvault, and genaisafe.

FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

# Install OCB
RUN go install go.opentelemetry.io/collector/cmd/builder@v0.104.0

WORKDIR /build
COPY ocb-config.yaml .

# OCB generates Go source and compiles the collector.
# The replace directives pull processors from GitHub at build time.
RUN builder --config=ocb-config.yaml --skip-strict-versioning

# --- Runtime stage ---
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/build/air-collector /usr/local/bin/air-collector
COPY collector.yaml /etc/otel/collector.yaml

EXPOSE 4317 4318

ENTRYPOINT ["air-collector"]
CMD ["--config=/etc/otel/collector.yaml"]
