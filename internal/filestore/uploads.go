package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"
)

// Uploads in parts carry contents bigger than one PUT of a blob takes (MaxFileSize), of any size unless the server
// sets a limit (SetMaxUpload):
//
//	GET    …/uploads                     {partSize, maxSize}: what the store takes (maxSize 0: no limit; 404 on a
//	                                     store without uploads)
//	POST   …/uploads {sha, size}         an upload, or the caller's one of the same contents under way with the parts
//	                                     it already has (a broken upload goes on from there); {done} when the room
//	                                     holds them
//	PUT    …/uploads/{id}/parts/{n}?sha= one part: partSize bytes (the last one the rest), hashing to sha, in order
//	GET    …/uploads/{id}                the upload's state (receiving, finishing, done)
//	POST   …/uploads/{id}/complete       the contents checked against the upload's sha and made a blob of the room:
//	                                     204, or 202 when that takes longer than a moment (then GET until done)
//	DELETE …/uploads/{id}                the upload given up
//
// The parts are written where the blob will be as they come (blobs_parts.go) and the sha256 of the contents is
// carried from part to part (its state is kept with the upload), so finishing reads nothing again. The caller and
// the room are the collection's, as for a PUT of a blob. An upload nobody touched for UploadTTL is removed with what
// it wrote.

const (
	// PartSize is the size of every part but the last, unless the contents take more than MaxParts of it.
	PartSize int64 = 8 << 20
	// MaxParts is the most parts an upload has (a bucket's multipart upload takes 10000).
	MaxParts = 10000
	// UploadTTL is how long an upload is kept after its last part.
	UploadTTL = 24 * time.Hour
	// MaxOpenUploads is how many uploads a room has under way at once; MaxOpenUploadsPerMember of them one member's.
	MaxOpenUploads          = 16
	MaxOpenUploadsPerMember = 4
	// SpareSpace is what a folder store keeps free on its disk past the uploads it takes.
	SpareSpace int64 = 256 << 20
	// finishWait is how long a complete waits for the contents to become a blob before it answers 202.
	finishWait = 15 * time.Second
	// finishTTL is how long an upload stays finishing; doneTTL how long a finished one answers its state.
	finishTTL = time.Hour
	doneTTL   = time.Hour
)

// The states of an upload.
const (
	UploadReceiving = "open"
	UploadFinishing = "finishing"
	UploadDone      = "done"
)

// ErrTooManyUploads is a room (or a member) with as many uploads under way as it may have.
var ErrTooManyUploads = errors.New("too many uploads under way")

// OpenLimits is how many uploads may be under way at once: in the room, and of one member (0: no limit).
type OpenLimits struct{ Room, Member int }

// UploadLimits is what a store takes in parts.
type UploadLimits struct {
	PartSize int64 `json:"partSize"`
	// MaxSize is the biggest upload; 0 is no limit.
	MaxSize int64 `json:"maxSize"`
}

// UploadRequest opens an upload.
type UploadRequest struct {
	Sha  string `json:"sha"`
	Size int64  `json:"size"`
}

// UploadPart is a part the store has.
type UploadPart struct {
	N    int    `json:"n"`
	Sha  string `json:"sha"`
	Size int64  `json:"size"`
	// Tag is what the blob store needs of the part to finish (a bucket's ETag).
	Tag string `json:"-"`
}

// UploadState answers an UploadRequest, and a GET of an upload.
type UploadState struct {
	ID       string       `json:"id,omitempty"`
	PartSize int64        `json:"partSize,omitempty"`
	Parts    []UploadPart `json:"parts,omitempty"`
	Expires  int64        `json:"expires,omitempty"`
	// State is receiving, finishing or done; Error what went wrong in the last finish that did not.
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
	// Done: the room holds the contents already; nothing is to be sent.
	Done bool `json:"done,omitempty"`
}

// Upload is an upload under way as the metadata keeps it.
type Upload struct {
	ID       string
	Member   string
	Sha      string
	Size     int64
	PartSize int64
	Created  int64
	Expires  int64
	// Handle names the assembly to the blob store; Hash is the sha256 state of the parts received (hex).
	Handle string
	Hash   string
	State  string
	Error  string
	Parts  []UploadPart
}

// PartCount is how many parts the contents take.
func (u Upload) PartCount() int { return int((u.Size + u.PartSize - 1) / u.PartSize) }

// PartLen is the size part n must have.
func (u Upload) PartLen(n int) int64 {
	if n == u.PartCount()-1 {
		return u.Size - int64(n)*u.PartSize
	}
	return u.PartSize
}

// received is how many bytes of the contents the store has.
func (u Upload) received() int64 {
	var n int64
	for _, p := range u.Parts {
		n += p.Size
	}
	return n
}

func (u Upload) state() UploadState {
	return UploadState{ID: u.ID, PartSize: u.PartSize, Parts: u.Parts, Expires: u.Expires, State: u.State, Error: u.Error}
}

// ValidUploadID: 32 lowercase hex.
func ValidUploadID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// SetMaxUpload sets the biggest upload in parts; 0 or less is no limit.
func (s *Server) SetMaxUpload(n int64) { s.maxUpload.Store(max(n, 0)) }

// SetPartSize sets the size of a part (0: PartSize). A bucket refuses parts under 5 MiB but the last.
func (s *Server) SetPartSize(n int64) { s.partSize = n }

func (s *Server) basePartSize() int64 {
	if s.partSize > 0 {
		return s.partSize
	}
	return PartSize
}

// partSizeFor is the part size of contents of size bytes: the base one, or a whole number of MiB that keeps them
// within MaxParts.
func (s *Server) partSizeFor(size int64) int64 {
	ps := s.basePartSize()
	if (size+ps-1)/ps <= MaxParts {
		return ps
	}
	need := (size + MaxParts - 1) / MaxParts
	return (need + 1<<20 - 1) &^ (1<<20 - 1)
}

func (s *Server) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// lockUpload serialises the requests of one upload in this process (the metadata keeps two replicas from recording
// the same part twice).
func (s *Server) lockUpload(id string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	mu := &s.uploadMu[h.Sum32()%uint32(len(s.uploadMu))]
	mu.Lock()
	return mu.Unlock
}

// hashAt is a sha256 that has read what state (hex of its binary form, "" for nothing) says.
func hashAt(state string) (hash.Hash, error) {
	h := sha256.New()
	if state == "" {
		return h, nil
	}
	b, err := hex.DecodeString(state)
	if err != nil {
		return nil, err
	}
	return h, h.(encoding.BinaryUnmarshaler).UnmarshalBinary(b)
}

func hashState(h hash.Hash) (string, error) {
	b, err := h.(encoding.BinaryMarshaler).MarshalBinary()
	return hex.EncodeToString(b), err
}

// lacksSpace answers 507 when a folder store's disk has less than need bytes free past SpareSpace.
func (s *Server) lacksSpace(w http.ResponseWriter, need int64) bool {
	sr, ok := s.blobs.(spaceReporter)
	if !ok {
		return false
	}
	free, ok := sr.FreeSpace()
	if !ok || free >= need+SpareSpace {
		return false
	}
	http.Error(w, fmt.Sprintf("not enough free space on the hub's disk: the upload needs %d MiB more, %d MiB are free (%d MiB are kept spare)",
		(need+1<<20-1)>>20, free>>20, SpareSpace>>20), http.StatusInsufficientStorage)
	return true
}

func (s *Server) uploadLimits(w http.ResponseWriter, _ *http.Request, _ caller) {
	writeJSON(w, http.StatusOK, UploadLimits{PartSize: s.basePartSize(), MaxSize: s.maxUpload.Load()})
}

func (s *Server) uploadOpen(w http.ResponseWriter, r *http.Request, c caller) {
	var req UploadRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req); err != nil || !ValidSha(req.Sha) || req.Size <= 0 {
		http.Error(w, "sha and size expected", http.StatusBadRequest)
		return
	}
	if lim := s.maxUpload.Load(); lim > 0 && req.Size > lim {
		http.Error(w, fmt.Sprintf("bigger than the store takes (%d bytes)", lim), http.StatusRequestEntityTooLarge)
		return
	}
	ctx := r.Context()
	if ok, err := c.store.HasBlob(ctx, req.Sha); err != nil {
		slog.Error("upload: has blob", "err", err)
		http.Error(w, "cannot read", http.StatusInternalServerError)
		return
	} else if ok {
		writeJSON(w, http.StatusOK, UploadState{Done: true})
		return
	}
	now := s.clock()
	c.store.sweepUploads(ctx, now.UnixMilli())
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	u, created, err := c.store.meta.UploadOpen(ctx, Upload{ID: hex.EncodeToString(id), Member: c.member, Sha: req.Sha,
		Size: req.Size, PartSize: s.partSizeFor(req.Size), Created: now.UnixMilli(), Expires: now.Add(UploadTTL).UnixMilli()},
		OpenLimits{Room: MaxOpenUploads, Member: MaxOpenUploadsPerMember}, now.UnixMilli())
	switch {
	case errors.Is(err, ErrTooManyUploads):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	case err != nil:
		slog.Error("upload: open", "err", err)
		http.Error(w, "cannot store", http.StatusInternalServerError)
		return
	}
	if s.lacksSpace(w, u.Size-u.received()) {
		if created {
			c.store.dropUpload(ctx, &u)
		}
		return
	}
	if created {
		handle, err := c.store.blobs.UploadBegin(ctx, c.room, u.ID, u.Sha)
		if err == nil {
			u.Handle = handle
			err = c.store.meta.UploadBegun(ctx, u.ID, handle)
		}
		if err != nil {
			slog.Error("upload: begin", "err", err)
			c.store.dropUpload(ctx, &u)
			http.Error(w, "cannot store", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, http.StatusOK, u.state())
}

// upload is the caller's upload the path names; nil after an answer was written.
func (s *Server) upload(w http.ResponseWriter, r *http.Request, c caller) *Upload {
	id := r.PathValue("id")
	if !ValidUploadID(id) {
		http.NotFound(w, r)
		return nil
	}
	u, err := c.store.meta.UploadGet(r.Context(), id, s.clock().UnixMilli())
	if err != nil {
		slog.Error("upload: get", "err", err)
		http.Error(w, "cannot read", http.StatusInternalServerError)
		return nil
	}
	// Another member's upload is not this caller's to add to or to finish.
	if u == nil || u.Member != c.member {
		http.Error(w, "no such upload", http.StatusNotFound)
		return nil
	}
	return u
}

func (s *Server) uploadGet(w http.ResponseWriter, r *http.Request, c caller) {
	if u := s.upload(w, r, c); u != nil {
		writeJSON(w, http.StatusOK, u.state())
	}
}

// countWriter counts what passes.
type countWriter struct{ n int64 }

func (c *countWriter) Write(b []byte) (int, error) { c.n += int64(len(b)); return len(b), nil }

func (s *Server) uploadPart(w http.ResponseWriter, r *http.Request, c caller) {
	defer s.lockUpload(r.PathValue("id"))()
	u := s.upload(w, r, c)
	if u == nil {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	sha := r.URL.Query().Get("sha")
	if err != nil || n < 0 || n >= u.PartCount() || !ValidSha(sha) {
		http.Error(w, "part number and sha expected", http.StatusBadRequest)
		return
	}
	if u.State != UploadReceiving {
		http.Error(w, "the upload is "+u.State, http.StatusConflict)
		return
	}
	next := len(u.Parts)
	switch {
	case n < next && u.Parts[n].Sha == sha:
		// Sent again (its answer was lost): the store has it.
		w.WriteHeader(http.StatusNoContent)
		return
	case n < next:
		http.Error(w, fmt.Sprintf("part %d is in with other contents", n), http.StatusConflict)
		return
	case n > next:
		http.Error(w, fmt.Sprintf("parts go in order: part %d is next", next), http.StatusConflict)
		return
	}
	size := u.PartLen(n)
	if s.lacksSpace(w, size) {
		return
	}
	whole, err := hashAt(u.Hash)
	if err != nil {
		slog.Error("upload: hash state", "err", err)
		http.Error(w, "cannot read", http.StatusInternalServerError)
		return
	}
	part, count := sha256.New(), &countWriter{}
	body := http.MaxBytesReader(w, r.Body, size+1)
	in := io.TeeReader(io.LimitReader(body, size), io.MultiWriter(whole, part, count))
	tag, err := c.store.blobs.UploadWrite(r.Context(), c.room, u, n, int64(n)*u.PartSize, size, in)
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, syscall.ENOSPC):
		http.Error(w, "not enough free space on the hub's disk", http.StatusInsufficientStorage)
		return
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, "no such upload", http.StatusNotFound)
		return
	case count.n != size:
		http.Error(w, fmt.Sprintf("the part is %d bytes, %d expected", count.n, size), http.StatusBadRequest)
		return
	case err != nil:
		slog.Error("upload: write part", "err", err)
		http.Error(w, "cannot store", http.StatusInternalServerError)
		return
	}
	if extra, err := body.Read(make([]byte, 1)); extra > 0 || errors.As(err, &tooBig) {
		http.Error(w, "the part is bigger than expected", http.StatusBadRequest)
		return
	}
	if hex.EncodeToString(part.Sum(nil)) != sha {
		http.Error(w, "the part does not hash to "+sha, http.StatusBadRequest)
		return
	}
	state, err := hashState(whole)
	if err != nil {
		slog.Error("upload: hash state", "err", err)
		http.Error(w, "cannot store", http.StatusInternalServerError)
		return
	}
	ok, err := c.store.meta.UploadPart(r.Context(), u.ID, UploadPart{N: n, Sha: sha, Size: size, Tag: tag}, state,
		s.clock().Add(UploadTTL).UnixMilli())
	switch {
	case err != nil:
		slog.Error("upload: record part", "err", err)
		http.Error(w, "cannot store", http.StatusInternalServerError)
	case !ok:
		http.Error(w, "another request took the part", http.StatusConflict)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) uploadComplete(w http.ResponseWriter, r *http.Request, c caller) {
	unlock := s.lockUpload(r.PathValue("id"))
	u := s.upload(w, r, c)
	if u == nil {
		unlock()
		return
	}
	switch u.State {
	case UploadDone:
		unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	case UploadFinishing:
		unlock()
		writeJSON(w, http.StatusAccepted, u.state())
		return
	}
	ctx := r.Context()
	if next := len(u.Parts); next < u.PartCount() {
		unlock()
		missing := make([]int, 0, u.PartCount()-next)
		for n := next; n < u.PartCount(); n++ {
			missing = append(missing, n)
		}
		http.Error(w, fmt.Sprintf("parts missing: %v", missing), http.StatusConflict)
		return
	}
	whole, err := hashAt(u.Hash)
	if err != nil || hex.EncodeToString(whole.Sum(nil)) != u.Sha {
		// The parts do not make the contents announced: they are of no use to anyone, the upload starts over.
		c.store.dropUpload(ctx, u)
		unlock()
		http.Error(w, "the parts do not hash to "+u.Sha, http.StatusBadRequest)
		return
	}
	now := s.clock()
	ok, err := c.store.meta.UploadSetState(ctx, u.ID, UploadReceiving, UploadFinishing, "", now.Add(finishTTL).UnixMilli())
	unlock()
	if err != nil {
		slog.Error("upload: finishing", "err", err)
		http.Error(w, "cannot store", http.StatusInternalServerError)
		return
	}
	u.State, u.Error = UploadFinishing, ""
	if !ok {
		writeJSON(w, http.StatusAccepted, u.state())
		return
	}
	// The finish goes on when the caller stops waiting: it answers 202 and asks for the state.
	done := make(chan error, 1)
	go func() {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTTL-5*time.Minute)
		defer cancel()
		err := c.store.blobs.UploadFinish(fctx, c.room, u)
		to, msg, expires := UploadDone, "", s.clock().Add(doneTTL)
		if err != nil {
			slog.Error("upload: finish", "room", c.room, "err", err)
			to, msg, expires = UploadReceiving, "the store could not finish the upload; complete it again", s.clock().Add(UploadTTL)
		}
		if _, serr := c.store.meta.UploadSetState(fctx, u.ID, UploadFinishing, to, msg, expires.UnixMilli()); serr != nil {
			slog.Error("upload: finished", "room", c.room, "err", serr)
		}
		done <- err
	}()
	wait := s.finishWait
	if wait <= 0 {
		wait = finishWait
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case err := <-done:
		if err != nil {
			http.Error(w, "cannot store", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case <-t.C:
		writeJSON(w, http.StatusAccepted, u.state())
	}
}

func (s *Server) uploadCancel(w http.ResponseWriter, r *http.Request, c caller) {
	defer s.lockUpload(r.PathValue("id"))()
	u := s.upload(w, r, c)
	if u == nil {
		return
	}
	if u.State == UploadFinishing {
		http.Error(w, "the upload is finishing", http.StatusConflict)
		return
	}
	c.store.dropUpload(r.Context(), u)
	w.WriteHeader(http.StatusNoContent)
}

// dropUpload removes what an upload wrote, then its record.
func (s *Store) dropUpload(ctx context.Context, u *Upload) {
	if u.State != UploadDone {
		if err := s.blobs.UploadAbort(ctx, s.room, u); err != nil {
			slog.Warn("upload: what it wrote was not removed, the upload stays to be removed again", "room", s.room, "err", err)
			return
		}
	}
	if err := s.meta.UploadDrop(ctx, u.ID); err != nil {
		slog.Warn("upload: drop", "room", s.room, "err", err)
	}
}

// sweepUploads removes the room's expired uploads.
func (s *Store) sweepUploads(ctx context.Context, now int64) int {
	ups, err := s.meta.UploadsExpired(ctx, now)
	if err != nil {
		slog.Warn("upload: expired uploads", "room", s.room, "err", err)
		return 0
	}
	for i := range ups {
		s.dropUpload(ctx, &ups[i])
	}
	return len(ups)
}

// SweepUploads removes the expired uploads of every room, at most once an hour and by one replica at a time.
func (s *Server) SweepUploads(ctx context.Context) int {
	now := s.clock()
	if last := s.lastSweep.Load(); last != 0 && now.Sub(time.UnixMilli(last)) < time.Hour {
		return 0
	}
	s.lastSweep.Store(now.UnixMilli())
	unlock, ok, err := s.meta.TryLock(ctx, "uploads")
	if err != nil || !ok {
		return 0
	}
	defer unlock()
	rooms, err := s.meta.Rooms(ctx)
	if err != nil {
		slog.Warn("upload sweep: rooms", "err", err)
		return 0
	}
	swept := 0
	for _, room := range rooms {
		if ctx.Err() != nil {
			break
		}
		st, err := s.Room(ctx, room)
		if err != nil {
			continue
		}
		swept += st.sweepUploads(ctx, now.UnixMilli())
	}
	return swept
}
