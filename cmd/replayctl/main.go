// Command replayctl replays an AIR record against the LLM provider
// and reports behavioral drift.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/airblackbox/gateway/pkg/recorder"
	"github.com/airblackbox/gateway/pkg/replay"
	"github.com/airblackbox/gateway/pkg/vault"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "replay" {
		fmt.Fprintf(os.Stderr, "Usage: replayctl replay <path/to/run.air.json>\n")
		os.Exit(1)
	}

	airPath := os.Args[2]

	rec, err := recorder.Load(airPath)
	if err != nil {
		log.Fatalf("load AIR record: %v", err)
	}

	fmt.Printf("Run ID:    %s\n", rec.RunID)
	fmt.Printf("Model:     %s\n", rec.Model)
	fmt.Printf("Provider:  %s\n", rec.Provider)
	fmt.Printf("Endpoint:  %s\n", rec.Endpoint)
	fmt.Printf("Tokens:    %d\n", rec.Tokens.Total)
	fmt.Printf("Status:    %s\n", rec.Status)
	fmt.Println()

	// Connect to vault.
	vaultEndpoint := envOr("VAULT_ENDPOINT", "localhost:9000")
	ctx := context.Background()
	vc, err := vault.New(ctx, vault.Config{
		Endpoint:  vaultEndpoint,
		AccessKey: envOr("VAULT_ACCESS_KEY", "minioadmin"),
		SecretKey: envOr("VAULT_SECRET_KEY", "minioadmin"),
		Bucket:    envOr("VAULT_BUCKET", "air-runs"),
		UseSSL:    envOr("VAULT_USE_SSL", "false") == "true",
	})
	if err != nil {
		log.Fatalf("vault connect: %v", err)
	}

	apiKey := envOr("OPENAI_API_KEY", "")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required for replay")
	}

	fmt.Println("Replaying...")
	result, err := replay.Run(ctx, rec, replay.Options{
		ProviderURL: envOr("PROVIDER_URL", "https://api.openai.com"),
		VaultClient: vc,
		APIKey:      apiKey,
	})
	if err != nil {
		log.Fatalf("replay failed: %v", err)
	}

	fmt.Println()
	fmt.Printf("Similarity: %.2f\n", result.Similarity)

	if result.Drift {
		fmt.Printf("DRIFT DETECTED: %s\n", result.DriftSummary)
		// Output full result as JSON for CI.
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		os.Exit(1)
	}

	fmt.Println("NO DRIFT — replay matches original within threshold.")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
