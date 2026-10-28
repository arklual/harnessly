// Package sitesclient deploys sites over the API of internal/sites without importing the server. The runtime and the
// agent publish with it.
package sitesclient

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
	"strings"
	"sync"
	"time"
)

// Client deploys sites to a server over the API (docs/hub/sites.md): the protocol of `harnessly site deploy`, for the
// code that has the files in memory, such as the runtime publishing a Sinopia project.
type Client struct {
	// Base is the server's address (https://hub.example.com), Token a deploy token, a publishing session or an
	// administrator's token.
	Base  string
	Token string
	HTTP  *http.Client
}

// ClientError is a refusal of the server, with the words it gave.
type ClientError struct {
	Status  int
	Code    string
	Message string
}

func (e *ClientError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the server answered %d", e.Status)
}

type file struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// DeployFile is one file of a deployment.
type DeployFile struct {
	Path string
	Data []byte
}

// Deployed is what the server answers to a finished deployment.
type Deployed struct {
	Version int    `json:"version"`
	URL     string `json:"url"`
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, ctype string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.Base, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode/100 != 2 {
		e := &ClientError{Status: res.StatusCode}
		var b struct{ Error, Message string }
		if json.Unmarshal(raw, &b) == nil {
			e.Code, e.Message = b.Error, b.Message
		}
		return e
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *Client) json(ctx context.Context, method, path string, in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return c.do(ctx, method, path, bytes.NewReader(raw), "application/json", out)
}

func sitePath(space, name string) string {
	return "/api/sites/" + url.PathEscape(space) + "/" + url.PathEscape(name)
}

// ErrNameTaken: the name belongs to a site this caller did not make for this origin.
var ErrNameTaken = errors.New("the site name is taken")

// Claim gets the site ready for a deployment of the thing named origin: it makes the site when there is none, and
// takes it over only when it was made for the same origin by this caller. A site of anybody else, or of another origin,
// is ErrNameTaken, and nothing of it is touched. access is used for a new site only.
func (c *Client) Claim(ctx context.Context, space, name, access, origin string) error {
	var have struct {
		Origin string `json:"origin"`
	}
	err := c.do(ctx, http.MethodGet, sitePath(space, name), nil, "", &have)
	var ce *ClientError
	switch {
	case err == nil:
		if origin == "" || have.Origin != origin {
			return ErrNameTaken
		}
		return nil
	case errors.As(err, &ce) && ce.Status == http.StatusForbidden:
		return ErrNameTaken // somebody else's site: the server shows a caller its own sites only
	case errors.As(err, &ce) && ce.Status == http.StatusNotFound:
		err = c.json(ctx, http.MethodPost, "/api/sites/"+url.PathEscape(space), map[string]string{"name": name, "access": access, "origin": origin}, nil)
		if errors.As(err, &ce) && ce.Status == http.StatusConflict {
			return ErrNameTaken
		}
		return err
	}
	return err
}

// SetAccess changes who may open the site.
func (c *Client) SetAccess(ctx context.Context, space, name, access string) error {
	return c.json(ctx, http.MethodPatch, sitePath(space, name), map[string]string{"access": access}, nil)
}

// Deploy sends the files as a new version of the site, which becomes the current one. Only the files the server does
// not have yet travel.
func (c *Client) Deploy(ctx context.Context, space, name, source string, files []DeployFile) (Deployed, error) {
	manifest := map[string]file{}
	byHash := map[string][]byte{}
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		h := hex.EncodeToString(sum[:])
		manifest[f.Path] = file{Hash: h, Size: int64(len(f.Data))}
		byHash[h] = f.Data
	}
	base := sitePath(space, name)
	var begun struct {
		ID      string   `json:"id"`
		Missing []string `json:"missing"`
	}
	if err := c.json(ctx, http.MethodPost, base+"/deploys", map[string]any{"files": manifest, "source": source}, &begun); err != nil {
		return Deployed{}, err
	}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
		sem   = make(chan struct{}, 8)
	)
	for _, h := range begun.Missing {
		data, ok := byHash[h]
		if !ok {
			return Deployed{}, fmt.Errorf("the server asked for a file we did not offer (%s)", h)
		}
		mu.Lock()
		stop := first != nil
		mu.Unlock()
		if stop {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			err := c.do(ctx, http.MethodPut, base+"/deploys/"+begun.ID+"/blobs/"+h, bytes.NewReader(data), "application/octet-stream", nil)
			if err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if first != nil {
		return Deployed{}, first
	}
	var done Deployed
	if err := c.json(ctx, http.MethodPost, base+"/deploys/"+begun.ID+"/commit", map[string]any{}, &done); err != nil {
		return Deployed{}, err
	}
	return done, nil
}
