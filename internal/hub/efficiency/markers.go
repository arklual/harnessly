package efficiency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// SourceMarker is the source of the markers in eff_source_facts: a fact per marker, keyed by its dedupe key, so putting the
// same marker again changes nothing. A marker is a dated change of how agents work (a model, a rule, a memory note) that the
// screen draws over the charts.
const SourceMarker = "marker"

var markerKinds = map[string]bool{"model": true, "claude": true, "plugins": true, "memory": true, "rule": true, "eval": true}

// Marker is IEffMarker.
type Marker struct {
	At     string  `json:"at"`
	Kind   string  `json:"kind"`
	Title  string  `json:"title"`
	Detail *string `json:"detail"`
}

type markerData struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// markersBetween reads the markers dated in [from, to), oldest first.
func markersBetween(ctx context.Context, st store.Store, from, to time.Time) ([]Marker, error) {
	facts, err := st.EffFactsBetween(ctx, SourceMarker, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	out := make([]Marker, 0, len(facts))
	for _, f := range facts {
		var d markerData
		if json.Unmarshal([]byte(f.Data), &d) != nil || !markerKinds[d.Kind] {
			continue
		}
		m := Marker{At: time.UnixMilli(f.At).UTC().Format(time.RFC3339), Kind: d.Kind, Title: d.Title}
		if d.Detail != "" {
			m.Detail = &d.Detail
		}
		out = append(out, m)
	}
	return out, nil
}

// markerIn is one marker of an import. Key makes a repeated import change nothing; without it the day, kind and title do.
type markerIn struct {
	Key    string `json:"key"`
	At     string `json:"at"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

const maxMarkers = 500

// ImportMarkers stores the markers of raw, a JSON array of {key?, at (RFC 3339), kind, title, detail?}, and says how many it
// stored. The instance's own markers come this way, from a file kept in its repository (HUB_EFFICIENCY_MARKERS), and every
// start stores them again: a marker with the same key is replaced, never doubled.
func ImportMarkers(ctx context.Context, st store.Store, raw string, now time.Time) (int, error) {
	var in []markerIn
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return 0, fmt.Errorf("markers: %w", err)
	}
	if len(in) > maxMarkers {
		return 0, fmt.Errorf("markers: at most %d", maxMarkers)
	}
	facts := make([]store.EffFact, 0, len(in))
	for i, m := range in {
		at, err := time.Parse(time.RFC3339, m.At)
		if err != nil {
			return 0, fmt.Errorf("markers[%d]: at must be RFC 3339", i)
		}
		title := strings.TrimSpace(m.Title)
		if !markerKinds[m.Kind] || title == "" || len(title) > 200 || len(m.Detail) > 1000 {
			return 0, fmt.Errorf("markers[%d]: bad kind or title", i)
		}
		key := m.Key
		if key == "" {
			sum := sha256.Sum256([]byte(at.UTC().Format("2006-01-02") + "\x00" + m.Kind + "\x00" + title))
			key = hex.EncodeToString(sum[:8])
		}
		data, _ := json.Marshal(markerData{Kind: m.Kind, Title: title, Detail: strings.TrimSpace(m.Detail)})
		facts = append(facts, store.EffFact{Source: SourceMarker, Key: key, At: at.UnixMilli(), Data: string(data)})
	}
	if err := st.UpsertEffFacts(ctx, facts, now.UnixMilli()); err != nil {
		return 0, err
	}
	return len(facts), nil
}
