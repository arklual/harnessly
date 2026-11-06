package sites

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// putFile writes a blob into a folder the way FolderBlobs does.
func putFile(t *testing.T, root, content string) string {
	t.Helper()
	h := sum([]byte(content))
	if err := os.MkdirAll(filepath.Join(root, h[:2]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, h[:2], h), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

// migrateSuite runs the move of a folder into dst; it is the same on the in-memory S3 and on MinIO.
func migrateSuite(t *testing.T, dst Blobs) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	a, b := putFile(t, root, "<h1>a</h1>"), putFile(t, root, "body{}")
	// A file whose content is not its name, and a file that is no blob at all, are not copied.
	bad := strings.Repeat("0", 64)
	_ = os.MkdirAll(filepath.Join(root, "00"), 0o700)
	_ = os.WriteFile(filepath.Join(root, "00", bad), []byte("tampered"), 0o600)
	_ = os.WriteFile(filepath.Join(root, "README"), []byte("x"), 0o600)

	rep, err := Migrate(ctx, root, dst, true, false)
	if err != nil || rep.Found != 3 || rep.Copied != 2 || len(rep.Skipped) != 1 {
		t.Fatalf("dry run: %+v %v", rep, err)
	}
	if ok, _ := dst.Has(ctx, a); ok {
		t.Fatal("a dry run wrote")
	}

	rep, err = Migrate(ctx, root, dst, false, false)
	if err != nil || rep.Copied != 2 || rep.Skipped[0] != bad {
		t.Fatalf("migrate: %+v %v", rep, err)
	}
	for _, h := range []string{a, b} {
		rc, _, err := dst.Open(ctx, h)
		if err != nil {
			t.Fatalf("%s not in the destination: %v", h, err)
		}
		rc.Close()
	}
	if ok, _ := dst.Has(ctx, bad); ok {
		t.Fatal("a tampered file was copied")
	}

	// Again: nothing to copy, and with removeSource the folder is emptied of what the destination holds.
	rep, err = Migrate(ctx, root, dst, false, true)
	if err != nil || rep.Copied != 0 || rep.Present != 2 || rep.Removed != 2 {
		t.Fatalf("second run: %+v %v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(root, a[:2], a)); !os.IsNotExist(err) {
		t.Fatalf("the source file stayed: %v", err)
	}
}

func TestMigrateToFakeS3(t *testing.T) {
	srv := fakeS3(t)
	dst, err := NewS3Blobs(S3Config{Endpoint: srv.URL, Bucket: "sites", Prefix: "p/sites/", AccessKey: "ak", SecretKey: "sk"})
	if err != nil {
		t.Fatal(err)
	}
	migrateSuite(t, dst)
}
