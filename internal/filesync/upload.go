package filesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// UploadLimits asks what the store takes in parts: a zero PartSize from a store without uploads in parts (an older
// one), a zero MaxSize from a store without a limit.
func (c *Client) UploadLimits(ctx context.Context) (filestore.UploadLimits, error) {
	resp, err := c.do(ctx, http.MethodGet, "/uploads", nil)
	if err != nil {
		return filestore.UploadLimits{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return filestore.UploadLimits{}, nil
	default:
		return filestore.UploadLimits{}, failure(resp)
	}
	var out filestore.UploadLimits
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<12)).Decode(&out)
	return out, err
}

// partTries is how many times one part is sent before the upload gives up (it goes on from there the next time).
var partTries = []time.Duration{0, time.Second, 5 * time.Second}

// maxPartSize is the biggest part the client holds in memory (a bucket's biggest part).
const maxPartSize = 5 << 30

// pollEvery is how often a finishing upload is asked about; finishTries how many times a finish that failed on the
// store's side is asked for again.
var (
	pollEvery   = 2 * time.Second
	finishTries = 3
)

// PutLarge uploads size bytes of r, which hash to sha, in parts. The store keeps the parts it got: a call after a
// broken one sends only the missing ones. ErrTooBig when the store does not take contents this big (or takes no
// uploads in parts). A store that takes a while to finish is asked until it has.
func (c *Client) PutLarge(ctx context.Context, sha string, size int64, r io.ReaderAt) error {
	for attempt := 0; ; attempt++ {
		err := c.putLarge(ctx, sha, size, r)
		// An upload that expired (or was dropped for a bad part) in the middle starts once more.
		if errors.Is(err, errUploadGone) && attempt == 0 {
			continue
		}
		return err
	}
}

var errUploadGone = errors.New("the store has no such upload any more")

func (c *Client) putLarge(ctx context.Context, sha string, size int64, r io.ReaderAt) error {
	st, err := c.openUpload(ctx, sha, size)
	if err != nil || st.Done {
		return err
	}
	if st.PartSize <= 0 || st.PartSize > maxPartSize {
		return fmt.Errorf("store: an upload with parts of %d bytes", st.PartSize)
	}
	if st.State == filestore.UploadFinishing || st.State == filestore.UploadDone {
		return c.awaitUpload(ctx, st.ID)
	}
	// The store takes parts in order: the ones it has are the first ones.
	count := int((size + st.PartSize - 1) / st.PartSize)
	buf := make([]byte, st.PartSize)
	for n := len(st.Parts); n < count; n++ {
		off := int64(n) * st.PartSize
		part := buf[:min(st.PartSize, size-off)]
		if _, err := r.ReadAt(part, off); err != nil && !(errors.Is(err, io.EOF) && n == count-1) {
			return err
		}
		sum := sha256.Sum256(part)
		if err := c.sendPart(ctx, st.ID, n, hex.EncodeToString(sum[:]), part); err != nil {
			return err
		}
	}
	return c.completeUpload(ctx, st.ID)
}

func (c *Client) openUpload(ctx context.Context, sha string, size int64) (filestore.UploadState, error) {
	body, _ := json.Marshal(filestore.UploadRequest{Sha: sha, Size: size})
	resp, err := c.do(ctx, http.MethodPost, "/uploads", body)
	if err != nil {
		return filestore.UploadState{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return filestore.UploadState{}, fmt.Errorf("%w: the store takes no uploads in parts", ErrTooBig)
	case http.StatusRequestEntityTooLarge:
		return filestore.UploadState{}, fmt.Errorf("%w: %v", ErrTooBig, failure(resp))
	default:
		return filestore.UploadState{}, failure(resp)
	}
	var st filestore.UploadState
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st)
	return st, err
}

func (c *Client) sendPart(ctx context.Context, id string, n int, sha string, data []byte) error {
	var err error
	for _, wait := range partTries {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		var resp *http.Response
		resp, err = c.do(ctx, http.MethodPut, "/uploads/"+id+"/parts/"+strconv.Itoa(n)+"?sha="+sha, data)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) || ctx.Err() != nil {
				return err
			}
			continue
		}
		code := resp.StatusCode
		if code == http.StatusNoContent {
			resp.Body.Close()
			return nil
		}
		err = failure(resp)
		resp.Body.Close()
		switch {
		case code == http.StatusNotFound:
			return fmt.Errorf("%w: %v", errUploadGone, err)
		case code < 500:
			return err
		}
	}
	return err
}

func (c *Client) completeUpload(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "/uploads/"+id+"/complete", []byte{})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusAccepted:
		return c.awaitUpload(ctx, id)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %v", errUploadGone, failure(resp))
	}
	return failure(resp)
}

// awaitUpload asks about a finishing upload until it is done; one whose finish failed on the store's side is
// completed again, a few times.
func (c *Client) awaitUpload(ctx context.Context, id string) error {
	tries := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
		st, err := c.uploadState(ctx, id)
		if err != nil {
			return err
		}
		switch st.State {
		case filestore.UploadDone:
			return nil
		case filestore.UploadFinishing:
			continue
		}
		if tries++; tries > finishTries {
			return fmt.Errorf("store: the upload was not finished: %s", st.Error)
		}
		resp, err := c.do(ctx, http.MethodPost, "/uploads/"+id+"/complete", []byte{})
		if err != nil {
			return err
		}
		code := resp.StatusCode
		ferr := failure(resp)
		resp.Body.Close()
		switch code {
		case http.StatusNoContent:
			return nil
		case http.StatusAccepted:
		case http.StatusNotFound:
			return fmt.Errorf("%w: %v", errUploadGone, ferr)
		default:
			return ferr
		}
	}
}

func (c *Client) uploadState(ctx context.Context, id string) (filestore.UploadState, error) {
	resp, err := c.do(ctx, http.MethodGet, "/uploads/"+id, nil)
	if err != nil {
		return filestore.UploadState{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return filestore.UploadState{}, fmt.Errorf("%w: %v", errUploadGone, failure(resp))
	default:
		return filestore.UploadState{}, failure(resp)
	}
	var st filestore.UploadState
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st)
	return st, err
}
