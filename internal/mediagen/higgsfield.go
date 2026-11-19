package mediagen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

// The Higgsfield API (docs.higgsfield.ai): the key as «Authorization: Key <id>:<secret>», a request by POST to the model's
// endpoint ID, its status by GET /requests/<id>/status, a cancel by POST /requests/<id>/cancel (202, only while it is
// queued), a price by POST /estimate/<model>, a file by a presigned upload (POST /files/generate-upload-url).

// errFatal is a failure that asking again does not mend (a refused key, a bad request, a missing request).
var errFatal = errors.New("fatal")

type hf struct {
	base, key string
	http      *http.Client
	allowHTTP bool
}

// apiError says what the provider answered, in words for whoever asked; fatal is true when retrying does not help.
func apiError(status int, raw []byte, what string) error {
	detail := ""
	var doc struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(raw, &doc) == nil && len(doc.Detail) > 0 {
		var s string
		if json.Unmarshal(doc.Detail, &s) == nil {
			detail = s
		} else {
			detail = strings.TrimSpace(string(doc.Detail))
		}
	}
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	var why string
	switch status {
	case http.StatusUnauthorized:
		why = "Higgsfield did not accept the key (check Settings → Providers → Higgsfield)"
	case http.StatusForbidden:
		why = "Higgsfield refused: the account's balance is too low or the key is off (top up the balance in console.higgsfield.ai)"
	case http.StatusNotFound:
		why = "Higgsfield has no such model or request for this account"
	case http.StatusTooManyRequests:
		why = "Higgsfield's queue of this account is full or the rate limit is hit: try again in a minute"
	case http.StatusLocked, http.StatusServiceUnavailable:
		why = "Higgsfield has this model blocked or not ready for now: try later or another model"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		why = "Higgsfield refused the request"
	default:
		why = "Higgsfield answered HTTP " + strconv.Itoa(status)
	}
	msg := what + ": " + why
	if detail != "" {
		msg += " — " + detail
	}
	if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
		return fmt.Errorf("%w: %s", errFatal, msg)
	}
	return errors.New(msg)
}

func (c *hf) addr(path string) string { return strings.TrimRight(c.base, "/") + path }

func (c *hf) do(ctx context.Context, method, target string, body any, extra http.Header, auth bool) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Key "+c.key)
	}
	for k, v := range extra {
		req.Header[k] = v
	}
	res, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return res.StatusCode, raw, nil
}

// estimate is the provider's price of a body, in USD.
func (c *hf) estimate(ctx context.Context, endpoint string, body map[string]any) (float64, error) {
	status, raw, err := c.do(ctx, http.MethodPost, c.addr("/estimate/"+endpoint), body, nil, true)
	if err != nil {
		return 0, fmt.Errorf("asking Higgsfield the price: %w", err)
	}
	if status != http.StatusOK {
		return 0, apiError(status, raw, "the price of the request")
	}
	var doc struct {
		USD json.RawMessage `json:"usd"`
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.USD) == 0 {
		return 0, fmt.Errorf("%w: Higgsfield's price is not readable", errFatal)
	}
	text := strings.Trim(string(doc.USD), `"`)
	usd, err := strconv.ParseFloat(text, 64)
	if err != nil || usd < 0 {
		return 0, fmt.Errorf("%w: Higgsfield's price %q is not a number", errFatal, text)
	}
	return usd, nil
}

// submit sends the request; the job's ID is its idempotency key, so a retry after a lost answer is the same request.
func (c *hf) submit(ctx context.Context, endpoint string, body map[string]any, idem string) (string, error) {
	h := http.Header{"Idempotency-Key": {"harnessly-" + idem}}
	var status int
	var raw []byte
	var err error
	for try := 0; try < 3; try++ {
		status, raw, err = c.do(ctx, http.MethodPost, c.addr("/"+endpoint), body, h, true)
		if err == nil && status < 500 {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("sending the request to Higgsfield: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated && status != http.StatusAccepted {
		return "", apiError(status, raw, "the request")
	}
	var doc struct {
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.RequestID == "" {
		return "", fmt.Errorf("%w: Higgsfield answered without a request id", errFatal)
	}
	return doc.RequestID, nil
}

type hfStatus struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Video  *struct {
		URL string `json:"url"`
	} `json:"video"`
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
	// URLs are the files a backend found itself (Higgsfield's MCP).
	URLs []string `json:"-"`
}

// urls are the files a finished request gives: the clip, or the pictures.
func (s hfStatus) urls() []string {
	out := append([]string(nil), s.URLs...)
	if s.Video != nil && s.Video.URL != "" {
		out = append(out, s.Video.URL)
	}
	for _, im := range s.Images {
		if im.URL != "" {
			out = append(out, im.URL)
		}
	}
	return out
}

func (c *hf) status(ctx context.Context, rid string) (hfStatus, error) {
	var st hfStatus
	target := c.addr("/requests/" + url.PathEscape(rid) + "/status")
	status, raw, err := c.do(ctx, http.MethodGet, target, nil, nil, true)
	if err != nil {
		return st, err
	}
	if status != http.StatusOK {
		return st, apiError(status, raw, "the status of the request")
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, fmt.Errorf("Higgsfield's status is not readable: %w", err)
	}
	return st, nil
}

// cancel stops a request that is still queued (202); one that is being made answers 400.
func (c *hf) cancel(ctx context.Context, rid string) error {
	status, raw, err := c.do(ctx, http.MethodPost, c.addr("/requests/"+url.PathEscape(rid)+"/cancel"), nil, nil, true)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusBadRequest:
		return errors.New("Higgsfield has begun the request: it cannot be canceled any more, it will end by itself")
	}
	return apiError(status, raw, "the cancel")
}

// upload puts a frame file into Higgsfield's storage and gives its public address: a presigned PUT, without the key.
func (c *hf) upload(ctx context.Context, path string) (string, error) {
	ct := frameExt[strings.ToLower(filepath.Ext(path))]
	if ct == "" {
		return "", errors.New("a frame is png, jpg, webp or gif")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	status, raw, err := c.do(ctx, http.MethodPost, c.addr("/files/generate-upload-url"), map[string]string{"content_type": ct}, nil, true)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", apiError(status, raw, "the upload address")
	}
	var up struct {
		PublicURL string            `json:"public_url"`
		UploadURL string            `json:"upload_url"`
		Headers   map[string]string `json:"upload_headers"`
	}
	if json.Unmarshal(raw, &up) != nil || up.PublicURL == "" || up.UploadURL == "" {
		return "", errors.New("Higgsfield's upload answer is not readable")
	}
	u, err := url.Parse(up.UploadURL)
	if err != nil || (u.Scheme != "https" && !c.allowHTTP) {
		return "", errors.New("Higgsfield's upload address is not https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, up.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	for k, v := range up.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", ct)
	}
	// The provider's key is not sent to the storage.
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode/100 != 2 {
		return "", fmt.Errorf("the storage refused the frame: HTTP %d", res.StatusCode)
	}
	return up.PublicURL, nil
}

var _ = extproviders.HiggsfieldBase
