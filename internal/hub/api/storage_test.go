package api_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// TestHubFilesInS3 starts a hub with HUB_STORAGE=s3 against a real MinIO (the CI job `minio-test` starts one and sets
// HUB_TEST_S3_*), publishes a site, and checks the files are in the bucket, under the hub's prefix, and not on disk.
func TestHubFilesInS3(t *testing.T) {
	endpoint, ak, sk := os.Getenv("HUB_TEST_S3_ENDPOINT"), os.Getenv("HUB_TEST_S3_ACCESS_KEY"), os.Getenv("HUB_TEST_S3_SECRET_KEY")
	if endpoint == "" {
		t.Skip("HUB_TEST_S3_ENDPOINT is not set: no MinIO to test against")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	c, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(ak, sk, ""), Secure: u.Scheme == "https", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bucket := "hub-test-" + strings.ToLower(t.Name())
	if ok, err := c.BucketExists(ctx, bucket); err != nil {
		t.Fatal(err)
	} else if !ok {
		if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	dataDir := t.TempDir()
	s := hubtest.New(t, hubtest.Env("HUB_DATA_DIR", dataDir), hubtest.Env("HUB_STORAGE", "s3"), hubtest.Env("HUB_S3_ENDPOINT", endpoint),
		hubtest.Env("HUB_S3_BUCKET", bucket), hubtest.Env("HUB_S3_ACCESS_KEY", ak), hubtest.Env("HUB_S3_SECRET_KEY", sk), hubtest.Env("HUB_S3_PREFIX", "hub/"))
	owner := s.SignIn("owner@example.com")
	_, slug := owner.NewSpace("Acme")
	if r := owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop", "access": "link"}); r.Code != 201 {
		t.Fatalf("create: %s", r)
	}
	publish(t, owner, slug, "shop", map[string]string{"index.html": "<h1>in the bucket</h1>"})

	// Served from the bucket.
	if r := s.Browser().Do("GET", "/sites/shop--"+slug+"/", nil); r.Code != 200 || string(r.Body) != "<h1>in the bucket</h1>" {
		t.Fatalf("served: %s", r)
	}
	// The object is under the hub's prefix, in the sites' part of it.
	want := "hub/sites/blobs/" + sum256("<h1>in the bucket</h1>")[:2] + "/" + sum256("<h1>in the bucket</h1>")
	if _, err := c.StatObject(ctx, bucket, want, minio.StatObjectOptions{}); err != nil {
		t.Fatalf("the object %s is not in the bucket: %v", want, err)
	}
	// And the data folder holds no copy.
	var onDisk []string
	_ = filepath.WalkDir(filepath.Join(dataDir, "sites"), func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			onDisk = append(onDisk, p)
		}
		return nil
	})
	if len(onDisk) != 0 {
		t.Errorf("files on disk with HUB_STORAGE=s3: %v", onDisk)
	}
}
