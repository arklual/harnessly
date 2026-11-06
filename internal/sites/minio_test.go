package sites

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// minioStore is a store over a fresh bucket of a real MinIO. The CI job `minio-test` starts one and sets
// HUB_TEST_S3_ENDPOINT, HUB_TEST_S3_ACCESS_KEY and HUB_TEST_S3_SECRET_KEY; without them these tests skip.
func minioStore(t *testing.T, prefix string) *S3Blobs {
	t.Helper()
	endpoint, ak, sk := os.Getenv("HUB_TEST_S3_ENDPOINT"), os.Getenv("HUB_TEST_S3_ACCESS_KEY"), os.Getenv("HUB_TEST_S3_SECRET_KEY")
	if endpoint == "" {
		t.Skip("HUB_TEST_S3_ENDPOINT is not set: no MinIO to test against")
	}
	b, err := NewS3Blobs(S3Config{Endpoint: endpoint, Bucket: "hub-test-" + strings.ToLower(t.Name()), Prefix: prefix, AccessKey: ak, SecretKey: sk})
	if err != nil {
		t.Fatal(err)
	}
	if err := makeTestBucket(context.Background(), endpoint, ak, sk, b.bucket); err != nil {
		t.Fatal(err)
	}
	return b
}

// makeTestBucket creates a bucket on a MinIO for a test; it is not an error that it is there already.
func makeTestBucket(ctx context.Context, endpoint, ak, sk, bucket string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	c, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(ak, sk, ""), Secure: u.Scheme == "https", BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return err
	}
	if ok, err := c.BucketExists(ctx, bucket); err != nil || ok {
		return err
	}
	return c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
}

func TestMinIOBlobs(t *testing.T) {
	b := minioStore(t, "p/")
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	blobSuite(t, b)
}

func TestMinIOMigrate(t *testing.T) {
	migrateSuite(t, minioStore(t, "hub/sites/"))
}
