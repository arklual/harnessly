package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// hubFake is the hub's uploads collection: contents by sha256, and how many times they were asked for.
type hubFake struct {
	blobs map[string][]byte
	asked int
}

func (h *hubFake) GetBlob(_ context.Context, sha string, w io.Writer) error {
	h.asked++
	b, ok := h.blobs[sha]
	if !ok {
		return filesync.ErrNoBlob
	}
	_, err := w.Write(b)
	return err
}

func shaOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hubUploadsOn(t *testing.T, hub *hubFake) v2Uploads {
	t.Helper()
	up := &wire.Uploads{Dir: t.TempDir(), Limits: wire.DefaultLimits}
	return v2Uploads{up: up, hub: func() hubBlobs {
		if hub == nil {
			return nil
		}
		return hub
	}}
}

// A file the hub holds and this machine does not is taken from the hub instead of refused, once.
func TestResolveTakesAHubUploadItLacks(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nnot really a picture")
	sha := shaOf(png)
	hub := &hubFake{blobs: map[string][]byte{sha: png}}
	u := hubUploadsOn(t, hub)
	id := "hub:" + sha + "?n=shot%201.png&t=image%2Fpng"

	refs, paths, code := u.Resolve([]string{id})
	if code != "" || len(refs) != 1 || len(paths) != 1 {
		t.Fatalf("Resolve = %v %v %q", refs, paths, code)
	}
	if refs[0].Name != "shot 1.png" || refs[0].Kind != wire.AttachmentImage || refs[0].Mime != "image/png" || !strings.HasPrefix(refs[0].ID, "up_") {
		t.Fatalf("ref = %+v", refs[0])
	}
	if got, err := os.ReadFile(paths[0]); err != nil || string(got) != string(png) {
		t.Fatalf("the file on the machine = %q, %v", got, err)
	}
	if !strings.HasSuffix(paths[0], ".png") {
		t.Fatalf("a picture goes in by its extension: %s", paths[0])
	}

	// The same file again (the next message, a retry) is the machine's own now: nothing is asked of the hub.
	refs2, _, code := u.Resolve([]string{id})
	if code != "" || refs2[0].ID != refs[0].ID || hub.asked != 1 {
		t.Fatalf("second Resolve = %+v %q, hub asked %d times", refs2, code, hub.asked)
	}
	// And the local id is an ordinary upload of this machine.
	if _, _, code := u.Resolve([]string{refs[0].ID}); code != "" {
		t.Fatalf("local id: %q", code)
	}
}

func TestResolveRefusesAHubUploadItCannotTake(t *testing.T) {
	good := []byte("hello")
	sha := shaOf(good)
	for name, tc := range map[string]struct {
		hub *hubFake
		id  string
	}{
		"not in the hub":      {&hubFake{blobs: map[string][]byte{}}, "hub:" + sha},
		"no hub's store":      {nil, "hub:" + sha},
		"bytes do not match":  {&hubFake{blobs: map[string][]byte{sha: []byte("other")}}, "hub:" + sha},
		"not a sha":           {&hubFake{}, "hub:xyz"},
		"a local id it lacks": {&hubFake{}, "up_NOPE"},
	} {
		t.Run(name, func(t *testing.T) {
			var u v2Uploads
			if tc.hub == nil {
				u = hubUploadsOn(t, nil)
			} else {
				u = hubUploadsOn(t, tc.hub)
			}
			if _, _, code := u.Resolve([]string{tc.id}); code != wire.RefuseAttachmentMissing {
				t.Fatalf("code = %q, want attachment-missing", code)
			}
		})
	}
}

func TestResolveHubUploadStaysAFile(t *testing.T) {
	b := []byte("a,b\n1,2\n")
	sha := shaOf(b)
	u := hubUploadsOn(t, &hubFake{blobs: map[string][]byte{sha: b}})
	refs, _, code := u.Resolve([]string{"hub:" + sha + "?n=t.csv&t=text%2Fcsv"})
	if code != "" || refs[0].Kind != wire.AttachmentFile || refs[0].Name != "t.csv" {
		t.Fatalf("ref = %+v %q", refs, code)
	}
}

func TestHubUploadFailsOnAHubError(t *testing.T) {
	sha := shaOf([]byte("x"))
	u := v2Uploads{up: &wire.Uploads{Dir: t.TempDir()}, hub: func() hubBlobs { return errBlobs{} }}
	if _, _, code := u.Resolve([]string{"hub:" + sha}); code != wire.RefuseAttachmentMissing {
		t.Fatalf("code = %q", code)
	}
}

type errBlobs struct{}

func (errBlobs) GetBlob(context.Context, string, io.Writer) error { return errors.New("hub down") }
