package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type s3Blobs struct {
	c      *minio.Client
	bucket string
	prefix string // no leading or trailing slash; may be empty
}

// NewS3Blobs opens an S3-compatible store from s3://bucket/prefix?endpoint=…&region=…&path_style=1.
//
//	endpoint    host[:port] or a full URL (https://s3.ru-3.storage.selcloud.ru); http:// means no TLS. Omitted: AWS.
//	region      the bucket's region (some servers want it for signing)
//	path_style  1 = bucket in the path (MinIO, most self-hosted servers); default is virtual-hosted style
//	credentials_file  an AWS-style credentials file (never the keys themselves)
//
// Keys come from the standard places and never from the URL: AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY (and
// AWS_SESSION_TOKEN), MINIO_ACCESS_KEY / MINIO_SECRET_KEY, the credentials file (AWS_SHARED_CREDENTIALS_FILE,
// AWS_PROFILE, ~/.aws/credentials) or the instance role.
//
// Blobs are immutable by sha256, so nothing here relies on conditional writes (If-None-Match): a blob is put when a
// HEAD says it is absent, and putting it twice writes the same bytes twice.
func NewS3Blobs(u *url.URL) (BlobStore, error) {
	bucket := u.Host
	if bucket == "" {
		return nil, errors.New("storage s3://: the bucket is missing (s3://bucket/prefix)")
	}
	q := u.Query()
	endpoint, secure := q.Get("endpoint"), true
	switch {
	case endpoint == "":
		endpoint = "s3.amazonaws.com"
	case strings.Contains(endpoint, "://"):
		eu, err := url.Parse(endpoint)
		if err != nil || eu.Host == "" {
			return nil, fmt.Errorf("storage endpoint %q", endpoint)
		}
		endpoint, secure = eu.Host, eu.Scheme != "http"
	}
	lookup := minio.BucketLookupDNS
	if v := q.Get("path_style"); v == "1" || v == "true" {
		lookup = minio.BucketLookupPath
	}
	chain := []credentials.Provider{
		&credentials.EnvAWS{},
		&credentials.EnvMinio{},
	}
	if f := q.Get("credentials_file"); f != "" {
		chain = append(chain, &credentials.FileAWSCredentials{Filename: f, Profile: os.Getenv("AWS_PROFILE")})
	}
	chain = append(chain, &credentials.FileAWSCredentials{}, &credentials.IAM{})
	c, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewChainCredentials(chain),
		Secure:       secure,
		Region:       q.Get("region"),
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	return &s3Blobs{c: c, bucket: bucket, prefix: strings.Trim(u.Path, "/")}, nil
}

// S3Config is a bucket with its own keys: what the hub's HUB_S3_* settings name.
type S3Config struct {
	Endpoint  string // http(s)://host[:port]
	Region    string
	Bucket    string
	Prefix    string // keys are <prefix>/rooms/<room>/blobs/ab/abcdef…; slashes around it do not matter
	AccessKey string
	SecretKey string
}

// NewS3BlobsWith opens a bucket with static keys and path-style addressing, which every S3-compatible service takes.
// It does not touch the bucket.
func NewS3BlobsWith(cfg S3Config) (BlobStore, error) {
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
	return &s3Blobs{c: c, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/")}, nil
}

func (s *s3Blobs) dir(room string) string { return path.Join(s.prefix, "rooms", room, "blobs") + "/" }

func (s *s3Blobs) key(room, sha string) string { return s.dir(room) + sha[:2] + "/" + sha }

func (s *s3Blobs) Close() error { return nil }

func notFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NotFound"
}

// busy is the bucket saying another write of the same key is under way (409 OperationAborted / "A conflicting
// conditional operation is currently in progress"): two machines putting the same blob at once. A blob is its sha256,
// so the other write puts the same bytes; the answer is to wait and look again, not a 500 (Selectel S3).
func busy(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusConflict || resp.Code == "OperationAborted" || resp.Code == "ConditionalRequestConflict"
}

// busyWaits are the pauses before each new try while the bucket is busy with the same key.
var busyWaits = []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

func pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *s3Blobs) Has(ctx context.Context, room, sha string) (bool, error) {
	if !ValidSha(sha) {
		return false, nil
	}
	_, err := s.c.StatObject(ctx, s.bucket, s.key(room, sha), minio.StatObjectOptions{})
	for i := 0; err != nil && busy(err) && i < len(busyWaits); i++ {
		if perr := pause(ctx, busyWaits[i]); perr != nil {
			return false, perr
		}
		_, err = s.c.StatObject(ctx, s.bucket, s.key(room, sha), minio.StatObjectOptions{})
	}
	if err != nil && notFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *s3Blobs) Open(ctx context.Context, room, sha string) (io.ReadCloser, error) {
	if !ValidSha(sha) {
		return nil, os.ErrNotExist
	}
	o, err := s.c.GetObject(ctx, s.bucket, s.key(room, sha), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy: a missing key shows on the first read, but a handler must answer 404 before it writes.
	if _, err := o.Stat(); err != nil {
		o.Close()
		if notFound(err) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	return o, nil
}

// Put hashes the contents into a temp file first (the size is needed up front, and nothing wrong may reach the
// bucket), then uploads it.
func (s *s3Blobs) Put(ctx context.Context, room, sha string, r io.Reader) error {
	if !ValidSha(sha) {
		return fmt.Errorf("%w: sha", ErrBadRequest)
	}
	if ok, err := s.Has(ctx, room, sha); err == nil && ok {
		_, err := io.Copy(io.Discard, io.LimitReader(r, MaxFileSize+1))
		return err
	}
	tmp, err := os.CreateTemp("", "filestore-put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := copyVerified(tmp, r, sha); err != nil {
		return err
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	put := func() error {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err := s.c.PutObject(ctx, s.bucket, s.key(room, sha), tmp, size, minio.PutObjectOptions{
			ContentType: "application/octet-stream",
		})
		return err
	}
	err = put()
	// Another write of the same blob is under way: once it is there, this one is done too; else it goes again.
	for i := 0; err != nil && busy(err) && i < len(busyWaits); i++ {
		if perr := pause(ctx, busyWaits[i]); perr != nil {
			return perr
		}
		if ok, herr := s.Has(ctx, room, sha); herr == nil && ok {
			return nil
		}
		err = put()
	}
	return err
}

func (s *s3Blobs) Delete(ctx context.Context, room, sha string) error {
	if !ValidSha(sha) {
		return nil
	}
	err := s.c.RemoveObject(ctx, s.bucket, s.key(room, sha), minio.RemoveObjectOptions{})
	if err != nil && notFound(err) {
		return nil
	}
	return err
}

func (s *s3Blobs) List(ctx context.Context, room string, fn func(Blob) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop() // an early return must not leave the listing goroutine behind
	dir := s.dir(room)
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: dir, Recursive: true}) {
		if o.Err != nil {
			return o.Err
		}
		sha := path.Base(o.Key)
		if !ValidSha(sha) || o.Key != dir+sha[:2]+"/"+sha {
			continue
		}
		if err := fn(Blob{Sha: sha, Size: o.Size, At: o.LastModified}); err != nil {
			return err
		}
	}
	return nil
}
