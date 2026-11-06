package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestStorageMigrateNeedsS3(t *testing.T) {
	var out, errOut bytes.Buffer
	e := env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": t.TempDir()})
	if code := run(context.Background(), []string{"storage", "migrate"}, e, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "HUB_STORAGE=s3") {
		t.Errorf("on the disk: %d %q", code, errOut.String())
	}
	errOut.Reset()
	if code := run(context.Background(), []string{"storage"}, e, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: hub storage migrate") {
		t.Errorf("no verb: %d %q", code, errOut.String())
	}
}

// TestStorageMigrateToMinIO moves a hub's sites from its data folder to a real MinIO (the CI job `minio-test`).
func TestStorageMigrateToMinIO(t *testing.T) {
	endpoint, ak, sk := os.Getenv("HUB_TEST_S3_ENDPOINT"), os.Getenv("HUB_TEST_S3_ACCESS_KEY"), os.Getenv("HUB_TEST_S3_SECRET_KEY")
	if endpoint == "" {
		t.Skip("HUB_TEST_S3_ENDPOINT is not set: no MinIO to test against")
	}
	u, _ := url.Parse(endpoint)
	c, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(ak, sk, ""), Secure: u.Scheme == "https", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "hub-test-" + strings.ToLower(t.Name())
	if ok, _ := c.BucketExists(context.Background(), bucket); !ok {
		if err := c.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	data := t.TempDir()
	sum := sha256.Sum256([]byte("<p>old site</p>"))
	h := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(filepath.Join(data, "sites", h[:2]), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(data, "sites", h[:2], h)
	if err := os.WriteFile(file, []byte("<p>old site</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": data, "HUB_STORAGE": "s3", "HUB_S3_ENDPOINT": endpoint,
		"HUB_S3_BUCKET": bucket, "HUB_S3_ACCESS_KEY": ak, "HUB_S3_SECRET_KEY": sk, "HUB_S3_PREFIX": "hub/"})
	migrate := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(context.Background(), append([]string{"storage", "migrate"}, args...), e, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, out, errOut := migrate("--dry-run"); code != 0 || !strings.Contains(out, "1 files") || !strings.Contains(out, "nothing written") {
		t.Fatalf("dry run: %d %q %q", code, out, errOut)
	}
	if _, err := c.StatObject(context.Background(), bucket, "hub/sites/blobs/"+h[:2]+"/"+h, minio.StatObjectOptions{}); err == nil {
		t.Fatal("a dry run wrote to the bucket")
	}
	if code, out, errOut := migrate(); code != 0 || !strings.Contains(out, "1 copied") {
		t.Fatalf("migrate: %d %q %q", code, out, errOut)
	}
	if _, err := c.StatObject(context.Background(), bucket, "hub/sites/blobs/"+h[:2]+"/"+h, minio.StatObjectOptions{}); err != nil {
		t.Fatalf("the file is not in the bucket: %v", err)
	}
	if code, out, _ := migrate("--remove-source"); code != 0 || !strings.Contains(out, "1 already in the bucket") || !strings.Contains(out, "1 removed") {
		t.Fatalf("remove source: %d %q", code, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the folder file stayed: %v", err)
	}
}
