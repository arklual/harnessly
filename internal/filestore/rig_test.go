package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"github.com/minio/minio-go/v7"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The store tests run on every driver combination that can be had here. SQLite + folder always. Postgres runs when
// HUB_TEST_POSTGRES_URL names a server (an admin URL: each test makes and drops a database of its own), S3 when
// HUB_TEST_S3_ENDPOINT names a MinIO with HUB_TEST_S3_ACCESS_KEY and HUB_TEST_S3_SECRET_KEY (the CI jobs hub-db and
// minio-test; each test works under a prefix of its own of one bucket and removes what it wrote). Nothing is faked: a
// combination whose variable is unset is skipped, and the skip says so.

const (
	envPG = "HUB_TEST_POSTGRES_URL"
	envS3 = "HUB_TEST_S3_ENDPOINT"
	// testBucket is the bucket the S3 combinations share.
	testBucket = "filestore-test"
)

type combo struct{ meta, blobs string } // "sqlite"|"postgres", "file"|"s3"

func (c combo) String() string { return c.meta + "+" + c.blobs }

func combos(t testing.TB) []combo {
	t.Helper()
	metas, blobs := []string{"sqlite"}, []string{"file"}
	if os.Getenv(envPG) != "" {
		metas = append(metas, "postgres")
	}
	if os.Getenv(envS3) != "" {
		blobs = append(blobs, "s3")
	}
	var out []combo
	for _, m := range metas {
		for _, b := range blobs {
			out = append(out, combo{m, b})
		}
	}
	return out
}

// eachCombo runs fn as a subtest on every available combination.
func eachCombo(t *testing.T, fn func(t *testing.T, r *rig)) {
	t.Helper()
	if os.Getenv(envPG) == "" {
		t.Logf("%s unset: Postgres combinations are NOT run", envPG)
	}
	if os.Getenv(envS3) == "" {
		t.Logf("%s unset: S3 combinations are NOT run", envS3)
	}
	for _, c := range combos(t) {
		t.Run(c.String(), func(t *testing.T) { fn(t, newRig(t, c)) })
	}
}

type rig struct {
	t       *testing.T
	c       combo
	dbURL   string
	storage string
	s3      *S3Config
	meta    MetaStore
	blobs   BlobStore
}

func randName() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newRig(t *testing.T, c combo) *rig {
	t.Helper()
	r := &rig{t: t, c: c}
	dir := t.TempDir()
	switch c.meta {
	case "sqlite":
		r.dbURL = "sqlite://" + filepath.Join(dir, "db")
	case "postgres":
		r.dbURL = newPGDatabase(t, os.Getenv(envPG))
	}
	switch c.blobs {
	case "file":
		r.storage = "file://" + filepath.Join(dir, "blobs")
	case "s3":
		r.s3 = &S3Config{Endpoint: os.Getenv(envS3), Bucket: testBucket, Prefix: "t-" + randName(),
			AccessKey: os.Getenv("HUB_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("HUB_TEST_S3_SECRET_KEY")}
		if err := makeTestBucket(context.Background(), *r.s3); err != nil {
			t.Fatal(err)
		}
	}
	r.meta, r.blobs = r.open()
	t.Cleanup(func() {
		ctx := context.Background()
		if c.blobs == "s3" { // leave the bucket as found
			rooms, _ := r.meta.Rooms(ctx)
			for _, room := range rooms {
				var shas []string
				_ = r.blobs.List(ctx, room, func(b Blob) error { shas = append(shas, b.Sha); return nil })
				for _, s := range shas {
					_ = r.blobs.Delete(ctx, room, s)
				}
			}
		}
		r.meta.Close()
		r.blobs.Close()
	})
	return r
}

// open opens the drivers again over the same data: what a second process, or a restart, sees.
func (r *rig) open() (MetaStore, BlobStore) {
	r.t.Helper()
	m, err := OpenMetaStore(context.Background(), r.dbURL)
	if err != nil {
		r.t.Fatal(err)
	}
	var b BlobStore
	var err2 error
	if r.s3 != nil {
		b, err2 = NewS3BlobsWith(*r.s3)
	} else {
		b, err2 = OpenBlobStore(r.storage)
	}
	if err := err2; err != nil {
		m.Close()
		r.t.Fatal(err)
	}
	return m, b
}

func newPGDatabase(t *testing.T, admin string) string {
	t.Helper()
	name := "dstest_" + randName()
	db, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db, err := sql.Open("pgx", admin)
		if err == nil {
			_, _ = db.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
			db.Close()
		}
	})
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	return u.String()
}

// store opens a room's Store on the rig's drivers.
func (r *rig) store(room string) *Store {
	r.t.Helper()
	rm, err := r.meta.Room(context.Background(), room)
	if err != nil {
		r.t.Fatal(err)
	}
	return NewStore(room, rm, r.blobs, PolicyFor(room))
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// put uploads contents and returns their sha.
func put(t testing.TB, s *Store, data string) string {
	t.Helper()
	sha := sum([]byte(data))
	if err := s.PutBlob(context.Background(), sha, strings.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	return sha
}

// write uploads contents and commits them as path on top of base.
func write(t testing.TB, s *Store, path string, base int64, data string) (*Entry, bool) {
	t.Helper()
	sha := put(t, s, data)
	e, conflict, err := s.Commit(context.Background(), CommitRequest{Path: path, Base: base, Sha: sha, Tag: sha, Size: int64(len(data)), Author: "t"}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	return e, conflict
}

// commitOf is a commit of already uploaded contents.
func commitOf(path string, base int64, sha string, size int64) CommitRequest {
	return CommitRequest{Path: path, Base: base, Sha: sha, Tag: sha, Size: size, Author: "t"}
}

// makeTestBucket creates the test bucket on a MinIO; it is not an error that it is there already.
func makeTestBucket(ctx context.Context, c S3Config) error {
	b, err := NewS3BlobsWith(c)
	if err != nil {
		return err
	}
	cl := b.(*s3Blobs).c
	if ok, err := cl.BucketExists(ctx, c.Bucket); err != nil || ok {
		return err
	}
	return cl.MakeBucket(ctx, c.Bucket, minio.MakeBucketOptions{})
}
