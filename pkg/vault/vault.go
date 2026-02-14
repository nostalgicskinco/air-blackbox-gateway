// Package vault provides S3-compatible blob storage for prompt/response content.
// Content is stored externally so traces contain only references, never raw data.
package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config holds S3-compatible storage configuration.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// Client wraps an S3-compatible object store.
type Client struct {
	mc     *minio.Client
	bucket string
}

// Ref is a vault reference returned after storing content.
type Ref struct {
	URI      string // vault://bucket/key
	Checksum string // sha256:hex
	Size     int64
}

// New creates a vault client and ensures the bucket exists.
func New(ctx context.Context, cfg Config) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("vault: connect: %w", err)
	}

	exists, err := mc.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("vault: check bucket: %w", err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("vault: create bucket: %w", err)
		}
	}

	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

// Store writes data to the vault and returns a reference with checksum.
func (c *Client) Store(ctx context.Context, key string, data []byte) (Ref, error) {
	h := sha256.Sum256(data)
	checksum := fmt.Sprintf("sha256:%x", h)

	info, err := c.mc.PutObject(ctx, c.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return Ref{}, fmt.Errorf("vault: store %s: %w", key, err)
	}

	return Ref{
		URI:      fmt.Sprintf("vault://%s/%s", c.bucket, key),
		Checksum: checksum,
		Size:     info.Size,
	}, nil
}

// Fetch retrieves content from the vault by key.
func (c *Client) Fetch(ctx context.Context, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("vault: fetch %s: %w", key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("vault: read %s: %w", key, err)
	}
	return data, nil
}

// VerifyChecksum re-computes sha256 of data and compares against expected.
func VerifyChecksum(data []byte, expected string) bool {
	h := sha256.Sum256(data)
	got := fmt.Sprintf("sha256:%x", h)
	return got == expected
}
