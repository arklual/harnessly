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
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// PutBlob uploads contents under their sha256.
func (c *Client) PutBlob(ctx context.Context, sha string, data []byte) error {
	resp, err := c.do(ctx, http.MethodPut, "/blobs/"+sha, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return failure(resp)
	}
	return nil
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

// Commit asks for a new revision; a stale base answers Conflict with the current revision.
func (c *Client) Commit(ctx context.Context, req filestore.CommitRequest) (filestore.CommitResponse, error) {
	var out filestore.CommitResponse
	body, _ := json.Marshal(req)
	resp, err := c.do(ctx, http.MethodPost, "/commit", body)
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
	var base int64
	for range 5 {
		resp, err := c.Commit(ctx, filestore.CommitRequest{Path: path, Base: base, Sha: sum, Tag: sum, Size: int64(len(data)), Author: author})
		if err != nil {
			return filestore.Entry{}, err
		}
		if !resp.Conflict {
			return *resp.Entry, nil
		}
		base = resp.Entry.Rev
	}
	return filestore.Entry{}, fmt.Errorf("store: %s keeps changing under the write", path)
}
