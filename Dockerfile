FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -o /replayctl ./cmd/replayctl

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /gateway /usr/local/bin/gateway
COPY --from=builder /replayctl /usr/local/bin/replayctl

EXPOSE 8080
ENTRYPOINT ["gateway"]
