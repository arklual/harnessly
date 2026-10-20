package nodejson

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// BodyUTF8 says whether a request's body is text in UTF-8 as its content type names it (no charset, or utf-8): the
// previous dispatcher decoded any other charset, which ParseBody does not.
func BodyUTF8(r *http.Request) bool {
	_, params, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	for _, p := range strings.Split(params, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
		if strings.EqualFold(k, "charset") {
			v = strings.ToLower(strings.ReplaceAll(v, `"`, ""))
			return v == "" || v == "utf-8" || v == "utf8" || v == "unicode-1-1-utf-8"
		}
	}
	return true
}

// ParseBody reads a body as the previous dispatcher did before a handler ran: refused past limit (413, "Body exceeded
// <limitName> limit"), decoded as UTF-8 without a leading BOM, then JSON (empty is {}) for a JSON type, a form as an
// object, anything else as text. A refusal is a status and its text/plain body; raw is the decoded text.
func ParseBody(r *http.Request, limit int64, limitName string) (v any, raw string, status int, refusal string) {
	tooLarge := "Body exceeded " + limitName + " limit"
	if r.ContentLength > limit {
		return nil, "", http.StatusRequestEntityTooLarge, tooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, "", http.StatusBadRequest, "Invalid body"
	}
	if int64(len(data)) > limit {
		return nil, "", http.StatusRequestEntityTooLarge, tooLarge
	}
	text := jscompat.DecodeUTF8(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/plain"
	}
	kind, _, _ := strings.Cut(ct, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "application/json", "application/ld+json":
		if text == "" {
			return jsvalue.NewObject(), text, 0, ""
		}
		v, err := jsvalue.Parse([]byte(text))
		if err != nil {
			return nil, text, http.StatusBadRequest, "Invalid JSON"
		}
		return v, text, 0, ""
	case "application/x-www-form-urlencoded":
		o := jsvalue.NewObject()
		for k, vs := range jscompat.Query(text) {
			if len(vs) == 1 {
				o.Set(k, vs[0])
			} else {
				o.Set(k, jsvalue.StringArray(vs))
			}
		}
		return o, text, 0, ""
	}
	return text, text, 0, ""
}
