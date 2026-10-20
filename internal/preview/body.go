package preview

import (
	"io"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// BodyLimit is the previous runtime's default request body limit.
const BodyLimit = 1 << 20

// BodyError is the dispatcher's plain-text refusal of a body: 413 too large, 400 not JSON.
type BodyError struct {
	Status int
	Text   string
}

// ParseBody reads a request body as the previous runtime's dispatcher did, before any handler ran: JSON (an empty body
// is {}), a form as an object, anything else as text. Objects keep JavaScript's key order.
func ParseBody(r *http.Request) (any, *BodyError) {
	tooLarge := &BodyError{413, "Body exceeded 1mb limit"}
	if r.ContentLength > BodyLimit {
		return nil, tooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, BodyLimit+1))
	if err != nil {
		return nil, &BodyError{400, "Invalid body"}
	}
	if len(raw) > BodyLimit {
		return nil, tooLarge
	}
	ct := strings.Join(r.Header.Values("Content-Type"), ", ")
	if ct == "" {
		ct = "text/plain"
	}
	kind := strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	text := jscompat.DecodeUTF8(raw)
	switch kind {
	case "application/json", "application/ld+json":
		if len(text) == 0 {
			return jsvalue.NewObject(), nil
		}
		v, err := jsvalue.Parse([]byte(text))
		if err != nil {
			return nil, &BodyError{400, "Invalid JSON"}
		}
		return v, nil
	case "application/x-www-form-urlencoded":
		out := jsvalue.NewObject()
		q := files.ParseQuery(text)
		for k, vs := range q {
			if len(vs) == 1 {
				out.Set(k, vs[0])
			} else {
				out.Set(k, jsvalue.StringArray(vs))
			}
		}
		return out, nil
	}
	return text, nil
}
