// Command gateway starts the AIR Blackbox Gateway — an OpenAI-compatible
// reverse proxy that records every LLM call for audit and replay.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/proxy"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/recorder"
	"github.com/nostalgicskinco/air-blackbox-gateway/pkg/vault"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", envOr("LISTEN_ADDR", ":8080"), "listen address")
	providerURL := flag.String("provider", envOr("PROVIDER_URL", "https://api.openai.com"), "upstream LLM provider")
	runsDir := flag.String("runs", envOr("RUNS_DIR", "./runs"), "AIR record output directory")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- OTel tracing setup ---
	tp, err := initTracer(ctx)
	if err != nil {
		log.Printf("WARN: OTel tracing disabled: %v", err)
	} else {
		defer tp.Shutdown(ctx)
	}

	// --- Vault setup (best-effort; gateway works without it) ---
	var vc *vault.Client
	vaultEndpoint := envOr("VAULT_ENDPOINT", "")
	if vaultEndpoint != "" {
		vc, err = vault.New(ctx, vault.Config{
			Endpoint:  vaultEndpoint,
			AccessKey: envOr("VAULT_ACCESS_KEY", "minioadmin"),
			SecretKey: envOr("VAULT_SECRET_KEY", "minioadmin"),
			Bucket:    envOr("VAULT_BUCKET", "air-runs"),
			UseSSL:    envOr("VAULT_USE_SSL", "false") == "true",
		})
		if err != nil {
			log.Printf("WARN: vault disabled: %v (gateway will proxy without recording)", err)
		} else {
			log.Printf("Vault connected: %s", vaultEndpoint)
		}
	} else {
		log.Println("WARN: VAULT_ENDPOINT not set — vault storage disabled")
	}

	// --- Recorder setup ---
	rec, err := recorder.NewWriter(*runsDir)
	if err != nil {
		log.Printf("WARN: AIR recording disabled: %v", err)
	} else {
		log.Printf("AIR records: %s", *runsDir)
	}

	// --- Gateway authentication ---
	gatewayKey := envOr("GATEWAY_KEY", "")
	if gatewayKey != "" {
		log.Println("Gateway authentication: enabled (X-Gateway-Key header required)")
	} else {
		log.Println("Gateway authentication: disabled (set GATEWAY_KEY to require auth)")
	}

	// --- Proxy handler ---
	handler := proxy.Handler(proxy.Config{
		ProviderURL: *providerURL,
		Vault:       vc,
		Recorder:    rec,
		GatewayKey:  gatewayKey,
	})

	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 180 * time.Second, // Allow time for slow LLM streaming responses.
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("AIR Blackbox Gateway listening on %s → %s", *addr, *providerURL)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	srv.Shutdown(shutCtx)
}

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
	endpoint := envOr("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if endpoint == "" {
		return nil, nil
	}

	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName("air-blackbox-gateway"),
		semconv.ServiceVersion("0.1.0"),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
