package realtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The models of the voice follow what the vendor lists: the key route asks the Realtime API's /v1/models
// with the machine's key, the subscription route takes the newest «gpt-live-…-codex» the machine's Codex knows
// (Config.LiveModels). The lists of Models stay the fallback and the floor: a model is never lost because a request
// failed.

// modelsTTL is how long a fetched list stands.
const modelsTTL = 6 * time.Hour

type modelsCache struct {
	mu   sync.Mutex
	at   time.Time
	key  []string
	busy bool
}

var dated = regexp.MustCompile(`-20\d\d-\d\d-\d\d$`)

// parseRealtimeModels picks the realtime models of an OpenAI /v1/models answer: «gpt-realtime…» without the dated
// snapshots, the newest first (the bigger version, then the full model before its mini).
func parseRealtimeModels(raw []byte) []string {
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []string
	for _, m := range doc.Data {
		id := strings.TrimSpace(m.ID)
		if !strings.HasPrefix(id, "gpt-realtime") || dated.MatchString(id) || strings.Contains(id, "preview") ||
			strings.Contains(id, "transcribe") || slices.Contains(out, id) {
			continue
		}
		out = append(out, id)
	}
	SortModels(out)
	return out
}

// SortModels orders model ids newest first: by the version in the id (gpt-realtime-2.1 > gpt-realtime-2 >
// gpt-realtime), a mini after its full model.
func SortModels(ids []string) {
	slices.SortStableFunc(ids, func(a, b string) int {
		if c := compareNums(modelVersion(b), modelVersion(a)); c != 0 {
			return c
		}
		am, bm := strings.Contains(a, "mini"), strings.Contains(b, "mini")
		switch {
		case am && !bm:
			return 1
		case bm && !am:
			return -1
		}
		return strings.Compare(a, b)
	})
}

var versionRe = regexp.MustCompile(`-(\d+(?:\.\d+)*)(?:-|$)`)

// modelVersion is the numbers of the first version in a model id: «gpt-live-1.1-codex» → [1 1]; none → nil.
func modelVersion(id string) []int {
	m := versionRe.FindStringSubmatch(id)
	if m == nil {
		return nil
	}
	var out []int
	for _, p := range strings.Split(m[1], ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

func compareNums(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := -1, -1
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// mergeModels is the fetched list with every fallback model it lacks after it, newest first.
func mergeModels(got, fallback []string) []string {
	out := slices.Clone(got)
	for _, m := range fallback {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	SortModels(out)
	return out
}

// modelsOf is the list a route offers now. The key route's list is refreshed in the background once it is older than
// modelsTTL; the call that finds it stale answers with what is kept.
func (s *Service) modelsOf(r string) []string {
	if r == RouteSubscription {
		live := []string{}
		if s.cfg.LiveModels != nil {
			for _, m := range s.cfg.LiveModels() {
				if strings.HasPrefix(m, "gpt-live-") {
					live = append(live, m)
				}
			}
		}
		out := mergeModels(live, Models[RouteSubscription])
		// The newest only: the subscription route has one model at a time, the older ones stop answering.
		return out[:1]
	}
	s.models.mu.Lock()
	got, stale := s.models.key, time.Since(s.models.at) > modelsTTL && !s.models.busy
	if stale {
		s.models.busy = true
	}
	s.models.mu.Unlock()
	if stale {
		go s.RefreshModels(context.Background())
	}
	return mergeModels(got, Models[RouteKey])
}

// RefreshModels asks the Realtime API which models the machine's key may use; a failure keeps what was kept.
func (s *Service) RefreshModels(ctx context.Context) {
	defer func() {
		s.models.mu.Lock()
		s.models.busy = false
		s.models.mu.Unlock()
	}()
	key, ok := s.key()
	if !ok {
		return
	}
	u, err := url.Parse(s.cfg.Endpoints.KeyCall)
	if err != nil || u.Host == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.Scheme+"://"+u.Host+"/v1/models", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	s.models.mu.Lock()
	defer s.models.mu.Unlock()
	s.models.at = time.Now()
	if resp.StatusCode/100 == 2 {
		if got := parseRealtimeModels(raw); len(got) > 0 {
			s.models.key = got
		}
	}
}
