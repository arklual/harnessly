package filesync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// Token gives the bearer a request to the store carries. refresh asks for a new one: the store refused the last.
type Token func(ctx context.Context, refresh bool) (string, error)

// ErrUnauthorized is a store that refused this machine even with a fresh token.
var ErrUnauthorized = errors.New("the store did not accept this machine")

// Client speaks the store's API for one collection: its base is the store's root (`https://hub.example.org/store`, or
// anything an in-process transport answers), the collection one of filestore.Kinds.
type Client struct {
	base  string
	coll  string
	token Token
	http  *http.Client
	// noBatch: the store has no batch look at the blobs (an older one), so each is asked for on its own.
	noBatch atomic.Bool
}

// NewClient makes a client of a collection. hc nil is a plain client with a timeout long enough for a long poll.
func NewClient(base, collection string, token Token, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Client{base: strings.TrimRight(base, "/"), coll: collection, token: token, http: hc}
}

// Collection is the client's collection kind.
func (c *Client) Collection() string { return c.coll }

// do calls an endpoint of the collection: path is relative to it ("/commit"). A 401 asks for a fresh token once.
func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		var rd io.Reader
		if body != nil {
			rd = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.base+"/v1/collections/"+c.coll+path, rd)
		if err != nil {
			return nil, err
		}
		if c.token != nil {
			tok, err := c.token(ctx, attempt > 0)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		resp.Body.Close()
		if attempt > 0 || c.token == nil {
			return nil, ErrUnauthorized
		}
	}
}

func failure(resp *http.Response) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("store: %s %s: %d %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, strings.TrimSpace(string(msg)))
}

// HasBlob tells whether the collection holds the contents with this sha256.
func (c *Client) HasBlob(ctx context.Context, sha string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, "/blobs/"+sha, nil)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, failure(resp)
}

// MissingBlobs tells which of the named contents the collection does not hold: one request for every
// filestore.MaxBlobCheck of them. A store that does not know the batch look is asked about each on its own, from then
// on without trying the batch again.
func (c *Client) MissingBlobs(ctx context.Context, shas []string) (map[string]bool, error) {
	missing := map[string]bool{}
	for len(shas) > 0 {
		part := shas
		if len(part) > filestore.MaxBlobCheck {
			part = part[:filestore.MaxBlobCheck]
		}
		shas = shas[len(part):]
		if !c.noBatch.Load() {
			done, err := c.missingBatch(ctx, part, missing)
			if err != nil {
				return nil, err
			}
			if done {
				continue
			}
			c.noBatch.Store(true)
		}
		for _, sha := range part {
			ok, err := c.HasBlob(ctx, sha)
			if err != nil {
				return nil, err
			}
			if !ok {
				missing[sha] = true
			}
		}
	}
	return missing, nil
}

// missingBatch asks about one batch; done is false when the store does not have the route.
func (c *Client) missingBatch(ctx context.Context, shas []string, missing map[string]bool) (done bool, err error) {
	body, _ := json.Marshal(filestore.MissingBlobsRequest{Shas: shas})
	resp, err := c.do(ctx, http.MethodPost, "/blobs/missing", body)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return false, nil
	default:
		return false, failure(resp)
	}
	var out filestore.MissingBlobsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return false, err
	}
	for _, sha := range out.Missing {
		missing[sha] = true
	}
	return true, nil
}

// PutBlob uploads contents under their sha256.
func (c *Client) PutBlob(ctx context.Context, sha string, data []byte) error {
	resp, err := c.do(ctx, http.MethodPut, "/blobs/"+sha, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		err := failure(resp)
		if tooBig(resp.StatusCode, err) {
			return fmt.Errorf("%w: %v", ErrTooBig, err)
		}
		return err
	}
	return nil
}

// ErrTooBig is contents bigger than the store takes: sending them again does not help.
var ErrTooBig = errors.New("the store does not take contents this big")

// tooBig: the store refused the contents for their size (413, or the 400 a store answers with its size limit).
func tooBig(status int, err error) bool {
	return status == http.StatusRequestEntityTooLarge ||
		status == http.StatusBadRequest && strings.Contains(err.Error(), "bigger than")
}

// ErrNoBlob is contents the collection does not hold.
var ErrNoBlob = errors.New("the store does not hold these contents")

// GetBlob writes the contents with this sha256 to w; ErrNoBlob when the collection does not hold them.
func (c *Client) GetBlob(ctx context.Context, sha string, w io.Writer) error {
	resp, err := c.do(ctx, http.MethodGet, "/blobs/"+sha, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNoBlob
	}
	if resp.StatusCode != http.StatusOK {
		return failure(resp)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// ErrRefused is a write the store does not take from this machine: the path is in a project none of the machine's
// people may edit.
var ErrRefused = errors.New("the store refuses this machine's write")

// Commit asks for a new revision; a stale base answers Conflict with the current revision.
func (c *Client) Commit(ctx context.Context, req filestore.CommitRequest) (filestore.CommitResponse, error) {
	var out filestore.CommitResponse
	body, _ := json.Marshal(req)
	resp, err := c.do(ctx, http.MethodPost, "/commit", body)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return out, fmt.Errorf("%w: %s", ErrRefused, req.Path)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return out, failure(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if out.Entry == nil {
		return out, fmt.Errorf("store: commit %s: empty answer", req.Path)
	}
	return out, nil
}

// Changes is every file changed after since; with wait, the store holds the request until something changes.
func (c *Client) Changes(ctx context.Context, since int64, wait time.Duration) (filestore.Changes, error) {
	var out filestore.Changes
	q := url.Values{"since": {strconv.FormatInt(since, 10)}, "wait": {strconv.Itoa(int(wait / time.Second))}}
	resp, err := c.do(ctx, http.MethodGet, "/changes?"+q.Encode(), nil)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, failure(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}

// Keys is the room key of a profile room as wrapped for each member that has it.
func (c *Client) Keys(ctx context.Context) ([]filestore.WrappedKey, error) {
	resp, err := c.do(ctx, http.MethodGet, "/keys", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, failure(resp)
	}
	var out filestore.WrappedKeys
	return out.Keys, json.NewDecoder(resp.Body).Decode(&out)
}

// Current is the latest revision of a path, nil when the path never existed.
func (c *Client) Current(ctx context.Context, path string) (*filestore.Entry, error) {
	resp, err := c.do(ctx, http.MethodGet, "/current?"+url.Values{"path": {path}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, failure(resp)
	}
	var e filestore.Entry
	return &e, json.NewDecoder(resp.Body).Decode(&e)
}

// Put stores data as the newest revision of path whatever was there (a file of a collection that nobody edits in
// place: an upload, a file an agent sent) and returns the revision; the contents are then read by Entry.Sha.
func (c *Client) Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error) {
	h := sha256.Sum256(data)
	sum := hex.EncodeToString(h[:])
	if ok, err := c.HasBlob(ctx, sum); err != nil {
		return filestore.Entry{}, err
	} else if !ok {
		if err := c.PutBlob(ctx, sum, data); err != nil {
			return filestore.Entry{}, err
		}
	}
	// The write starts from the revision this process last saw of the path, so writing a path again is not first
	// refused as stale. A conflict names the current revision: the write is tried on top of it after a short spread
	// wait, and a path that still moves is left to the caller.
	key := c.base + "\x00" + c.coll + "\x00" + path
	base := heads.get(key)
	for attempt := 0; ; attempt++ {
		resp, err := c.Commit(ctx, filestore.CommitRequest{Path: path, Base: base, Sha: sum, Tag: sum, Size: int64(len(data)), Author: author})
		if err != nil {
			return filestore.Entry{}, err
		}
		heads.set(key, resp.Entry.Rev)
		if !resp.Conflict {
			return *resp.Entry, nil
		}
		if attempt >= putRetries {
			break
		}
		base = resp.Entry.Rev
		if attempt > 0 {
			if !sleep(ctx, putRetryWait+rand.N(putRetryWait)) {
				return filestore.Entry{}, ctx.Err()
			}
		}
	}
	return filestore.Entry{}, fmt.Errorf("store: %s keeps changing under the write", path)
}

// putRetries is how many times Put writes again on top of the revision a conflict named; the first goes at once (the
// path was simply not known yet), the next after putRetryWait and as much again at random.
const putRetries = 2

var putRetryWait = 300 * time.Millisecond

// heads is the newest revision this process saw of the paths it puts whole, by store, collection and path.
var heads = &headMemory{revs: map[string]int64{}}

type headMemory struct {
	mu   sync.Mutex
	revs map[string]int64
}

const headMemoryMost = 4096

func (h *headMemory) get(key string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.revs[key]
}

func (h *headMemory) set(key string, rev int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.revs[key]; !ok && len(h.revs) >= headMemoryMost {
		clear(h.revs)
	}
	h.revs[key] = rev
}

// History is the revisions of a path, newest first; empty when the path never existed or is not this machine's to see.
func (c *Client) History(ctx context.Context, path string) ([]filestore.Entry, error) {
	resp, err := c.do(ctx, http.MethodGet, "/history?"+url.Values{"path": {path}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return []filestore.Entry{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, failure(resp)
	}
	var out []filestore.Entry
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// HistoryUnder is the newest revisions of the paths under a folder (prefix ends in "/"; "" for the whole collection).
func (c *Client) HistoryUnder(ctx context.Context, prefix string, limit int) ([]filestore.Entry, error) {
	q := url.Values{"prefix": {prefix}, "limit": {strconv.Itoa(limit)}}
	resp, err := c.do(ctx, http.MethodGet, "/history?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, failure(resp)
	}
	var out []filestore.Entry
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// Snapshot is a folder (prefix ends in "/") as it was at a revision.
func (c *Client) Snapshot(ctx context.Context, prefix string, at int64) (filestore.Snapshot, error) {
	var out filestore.Snapshot
	q := url.Values{"prefix": {prefix}, "at": {strconv.FormatInt(at, 10)}}
	resp, err := c.do(ctx, http.MethodGet, "/snapshot?"+q.Encode(), nil)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, failure(resp)
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// TakeLease takes the lease on req.Key for this machine's req.Chat, or refreshes it; Held says someone else's live
// lease is there (then Lease is theirs).
func (c *Client) TakeLease(ctx context.Context, req filestore.LeaseRequest) (filestore.LeaseResponse, error) {
	var out filestore.LeaseResponse
	body, _ := json.Marshal(req)
	resp, err := c.do(ctx, http.MethodPost, "/leases", body)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return out, failure(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	out.Held = out.Held || resp.StatusCode == http.StatusConflict
	return out, nil
}
