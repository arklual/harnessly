package sites

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config is an S3-compatible bucket: MinIO for a self-hosted install, Selectel for the cloud.
type S3Config struct {
	Endpoint  string // https://s3.example.com (or http://minio:9000)
	Region    string // empty: the service's default
	Bucket    string
	Prefix    string // keys are <prefix>blobs/ab/abcdef…
	AccessKey string
	SecretKey string
}

// S3Blobs keeps blobs in an S3 bucket. Path-style addressing, which every S3-compatible service takes.
type S3Blobs struct {
	c      *minio.Client
	bucket string
	prefix string
}

// NewS3Blobs makes the store. It does not touch the bucket; Check does.
func NewS3Blobs(cfg S3Config) (*S3Blobs, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("s3 endpoint %q: need http(s)://host[:port]", cfg.Endpoint)
	}
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("s3: bucket, access key and secret key are required")
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       u.Scheme == "https",
		Region:       cfg.Region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, err
	}
	return &S3Blobs{c: c, bucket: cfg.Bucket, prefix: cfg.Prefix}, nil
}

// Check says whether the bucket exists and the keys may use it.
func (s *S3Blobs) Check(ctx context.Context) error {
	ok, err := s.c.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("s3 bucket %s: %w", s.bucket, err)
	}
	if !ok {
		return fmt.Errorf("s3 bucket %s does not exist", s.bucket)
	}
	return nil
}

func (s *S3Blobs) key(hash string) (string, error) {
	if !ValidHash(hash) {
		return "", fmt.Errorf("blob %q: not a sha256", hash)
	}
	return s.prefix + "blobs/" + hash[:2] + "/" + hash, nil
}

func isNoSuchKey(err error) bool {
	r := minio.ToErrorResponse(err)
	return r.StatusCode == 404 || strings.EqualFold(r.Code, "NoSuchKey")
}

func (s *S3Blobs) Has(ctx context.Context, hash string) (bool, error) {
	k, err := s.key(hash)
	if err != nil {
		return false, err
	}
	_, err = s.c.StatObject(ctx, s.bucket, k, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if isNoSuchKey(err) {
		return false, nil
	}
	return false, err
}

func (s *S3Blobs) Put(ctx context.Context, hash string, r io.Reader, size int64) error {
	k, err := s.key(hash)
	if err != nil {
		return err
	}
	_, err = s.c.PutObject(ctx, s.bucket, k, r, size, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *S3Blobs) Open(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	k, err := s.key(hash)
	if err != nil {
		return nil, 0, err
	}
	obj, err := s.c.GetObject(ctx, s.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	// GetObject is lazy: the first Stat is the request.
	st, err := obj.Stat()
	if err != nil {
		obj.Close()
		if isNoSuchKey(err) {
			return nil, 0, ErrBlobNotFound
		}
		return nil, 0, err
	}
	return obj, st.Size, nil
}

func (s *S3Blobs) Delete(ctx context.Context, hash string) error {
	k, err := s.key(hash)
	if err != nil {
		return err
	}
	return s.c.RemoveObject(ctx, s.bucket, k, minio.RemoveObjectOptions{})
}
