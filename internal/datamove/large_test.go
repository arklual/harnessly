package datamove

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// largeStore takes objects bigger than one request in parts, up to limit (0: none, an older hub).
type largeStore struct {
	*fakeStore
	limit  int64
	larges int
}

func (s *largeStore) LargeLimit(context.Context, string) (int64, error) { return s.limit, nil }

func (s *largeStore) PutLarge(_ context.Context, kind, sha string, size int64, r io.ReaderAt) error {
	if size > s.limit {
		return fmt.Errorf("%w: 413", ErrTooBig)
	}
	data := make([]byte, size)
	if _, err := r.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != sha {
		return errors.New("the bytes do not hash to the name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.larges++
	s.blobs[kind+"/"+sha] = data
	return nil
}

// An object skipped as too big moves by itself in parts once the store takes it, and not before.
func TestATooBigObjectMovesInPartsOnceTheStoreTakesIt(t *testing.T) {
	data, transcripts := oldInstall(t)
	big := "png-2, grown bigger than one request carries"
	write(t, filepath.Join(data, "v2", "uploads", "up_2"), big)
	st := &largeStore{fakeStore: &fakeStore{blobs: map[string][]byte{}}}
	j := &fakeJournal{}
	m := newMover(data, transcripts, st.fakeStore, j)
	m.o.Target = st
	m.o.MaxBytes = 25

	// An older hub: skipped, and not tried again pass after pass.
	m.pass(context.Background())
	m.pass(context.Background())
	if s := m.Status(); s.Skipped != 1 || st.larges != 0 {
		t.Fatalf("status = %+v, %d uploads in parts; want one skipped and none sent", s, st.larges)
	}
	if o := j.last("uploads", "up_2"); o == nil || o.Code != CodeTooBig {
		t.Fatalf("the hub was told %+v of the big upload", o)
	}

	// A hub that takes less than the object still refuses it, once.
	st.limit = 30
	m.pass(context.Background())
	m.pass(context.Background())
	if s := m.Status(); s.Skipped != 1 || st.larges != 0 {
		t.Fatalf("status = %+v under a limit below the object", s)
	}

	// The hub updated, without a limit: the object goes in parts and is verified.
	st.limit = math.MaxInt64
	m.pass(context.Background())
	if s := m.Status(); s.Skipped != 0 || s.Verified != 4 || st.larges != 1 {
		t.Fatalf("status = %+v, %d uploads in parts after the hub took more", s, st.larges)
	}
	if got := string(st.blobs["uploads/"+shaOf(big)]); got != big {
		t.Fatalf("the store holds %q", got)
	}
	if o := j.last("uploads", "up_2"); o == nil || o.State != hubproto.MigrationVerified {
		t.Fatalf("the hub was told %+v after the move", o)
	}
}

// A journal written before the limit was kept counts its refusals under the single request's limit.
func TestAnOlderJournalsTooBigMovesOnceTheHubTakesMore(t *testing.T) {
	data, transcripts := oldInstall(t)
	big := "png-2, grown bigger than one request carries"
	write(t, filepath.Join(data, "v2", "uploads", "up_2"), big)
	st := &largeStore{fakeStore: &fakeStore{blobs: map[string][]byte{}}}
	m := newMover(data, transcripts, st.fakeStore, &fakeJournal{})
	m.o.Target = st
	m.o.MaxBytes = 25
	m.pass(context.Background())
	m.mu.Lock()
	m.st.Records[key(KindUploads, "up_2")].Limit = 0
	m.mu.Unlock()
	m.pass(context.Background())
	if st.larges != 0 {
		t.Fatal("an older hub was sent the object in parts")
	}
	st.limit = 1 << 20
	m.pass(context.Background())
	if s := m.Status(); s.Skipped != 0 || st.larges != 1 {
		t.Fatalf("status = %+v, %d uploads in parts", s, st.larges)
	}
}
