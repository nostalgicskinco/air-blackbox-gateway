package vault

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	data := []byte(`{"role":"user","content":"hello"}`)

	h := sha256.Sum256(data)
	good := fmt.Sprintf("sha256:%x", h)

	if !VerifyChecksum(data, good) {
		t.Fatal("expected checksum to match")
	}

	if VerifyChecksum(data, "sha256:0000") {
		t.Fatal("expected checksum mismatch")
	}
}

func TestRefFields(t *testing.T) {
	r := Ref{
		URI:      "vault://air-runs/abc/request.json",
		Checksum: "sha256:deadbeef",
		Size:     42,
	}
	if r.URI == "" || r.Checksum == "" || r.Size != 42 {
		t.Fatal("ref fields not set")
	}
}
