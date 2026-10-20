package timeline

import (
	"errors"
	"math"
	"os"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const maxUserMessageLength = 200

func entryType(e *jsvalue.Object) string { return e.String("type") }

// findLastUserMessage is the newest typed message of entries, trimmed and cut for the tab's preview; "" for none.
func findLastUserMessage(entries []*jsvalue.Object) string {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if entryType(e) != "user-message" {
			continue
		}
		text, _ := e.Get("text")
		s, ok := text.(string)
		if !ok {
			continue
		}
		if t := jscompat.Trim(s); t != "" {
			if jscompat.Len(t) > maxUserMessageLength {
				return jscompat.Slice(t, maxUserMessageLength) + "…"
			}
			return t
		}
	}
	return ""
}

var errInvalidDate = errors.New("Invalid time value")

func timestampOf(e *jsvalue.Object) any {
	v, has := e.Get("timestamp")
	if !has {
		return nil
	}
	return v
}

// dateOf is new Date(v).getTime() for an entry's timestamp (a number in practice; undefined is NaN).
func dateOf(v any) float64 {
	switch x := v.(type) {
	case nil:
		return math.NaN()
	case float64:
		return x
	}
	return jscompat.DateValue(v)
}

// computeInitMeta is the first screen's summary of what it shows. Like the old runtime it fails on an entry whose time
// is not a date (toISOString throws there).
func computeInitMeta(entries []*jsvalue.Object, fileSize int64, createdAtOverride *string, customTitle *string) (*jsvalue.Object, error) {
	var createdAt, updatedAt any
	lastTimestamp := 0.0
	userCount, assistantCount := 0, 0
	for _, e := range entries {
		ts := timestampOf(e)
		if createdAt == nil && jsvalue.Truthy(ts) {
			iso, ok := jscompat.ISOString(dateOf(ts))
			if !ok {
				return nil, errInvalidDate
			}
			createdAt = iso
		}
		if jsvalue.Truthy(ts) {
			lastTimestamp = jsMax(lastTimestamp, numberOf(ts))
		}
		iso, ok := jscompat.ISOString(dateOf(ts))
		if !ok {
			return nil, errInvalidDate
		}
		updatedAt = iso
		switch entryType(e) {
		case "user-message":
			userCount++
		case "assistant-message":
			assistantCount++
		}
	}
	meta := jsvalue.NewObject()
	if createdAtOverride != nil {
		meta.Set("createdAt", *createdAtOverride)
	} else {
		meta.Set("createdAt", createdAt)
	}
	meta.Set("updatedAt", updatedAt)
	meta.Set("lastTimestamp", lastTimestamp)
	meta.Set("fileSize", float64(fileSize))
	meta.Set("userCount", float64(userCount))
	meta.Set("assistantCount", float64(assistantCount))
	if customTitle != nil {
		meta.Set("customTitle", *customTitle)
	}
	return meta, nil
}

// numberOf is Number(v) for an entry's time, a number in practice.
func numberOf(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return math.NaN()
}

// jsMax is Math.max of two numbers: NaN wins.
func jsMax(a, b float64) float64 {
	if math.IsNaN(a) || math.IsNaN(b) {
		return math.NaN()
	}
	return math.Max(a, b)
}

// readFirstTimestamp is the time of a transcript's first line when that line carries one: the first 4097 bytes are
// read, so a longer first line is no line at all.
func readFirstTimestamp(path string) *string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 4097)
	n, _ := f.Read(buf)
	text := decodeUTF8(buf[:n])
	for _, line := range splitReadline(text) {
		if jscompat.Trim(line) == "" {
			continue
		}
		v, err := jsvalue.Parse([]byte(line))
		if err != nil {
			return nil
		}
		o, ok := v.(*jsvalue.Object)
		if !ok {
			return nil
		}
		ts, _ := o.Get("timestamp")
		if !jsvalue.Truthy(ts) {
			return nil
		}
		iso, ok := jscompat.ISOString(dateOf(ts))
		if !ok {
			return nil
		}
		return &iso
	}
	return nil
}

// decodeUTF8 is Buffer#toString('utf-8').
func decodeUTF8(b []byte) string { return jscompat.DecodeUTF8(b) }

// splitReadline splits text into lines the way readline does: at \n, \r\n and a lone \r.
func splitReadline(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			lines = append(lines, text[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, text[start:i])
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}
