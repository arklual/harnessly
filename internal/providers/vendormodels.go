package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The presets' model lists follow what the vendor's /models lists: a newer model of a family the preset
// already has (Grok 4.8 beside Grok 4.7, GLM 5.4 beside GLM 5.3) is offered the day the vendor ships it, with the
// window and efforts of its older sibling, without an edit here. A model of a family the preset does not know, an
// older one or a non-chat one (images, speech, embeddings) is not added: the vendors list dozens. Nothing is removed:
// a list the key cannot see in full never takes a model away.

// vendorModelsURL is where a preset's vendor lists its models; always on BaseURL's host (checked), so a key goes
// nowhere it does not already go.
var vendorModelsURL = map[string]string{
	"xai":       "https://api.x.ai/v1/models",
	"kimi":      "https://api.moonshot.ai/v1/models",
	"kimi-code": "https://api.kimi.com/coding/v1/models",
	"glm":       "https://api.z.ai/api/paas/v4/models",
	"glm-cn":    "https://open.bigmodel.cn/api/paas/v4/models",
}

// VendorModelsEvery is how often the lists are asked again.
const VendorModelsEvery = 6 * time.Hour

// VendorModel is a model as the vendor lists it.
type VendorModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

var vendorModels struct {
	sync.RWMutex
	file   string
	loaded bool
	At     map[string]int64         `json:"at"`
	Lists  map[string][]VendorModel `json:"lists"`
}

// ParseVendorModels reads an OpenAI- or Anthropic-style list: {"data":[{"id":…,"display_name"?:…}]}.
func ParseVendorModels(raw []byte) []VendorModel {
	var doc struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	var out []VendorModel
	for _, m := range doc.Data {
		id := strings.TrimSpace(m.ID)
		if !vendorModelID.MatchString(id) || slices.ContainsFunc(out, func(x VendorModel) bool { return x.ID == id }) {
			continue
		}
		name := strings.TrimSpace(m.DisplayName)
		if name == "" {
			name = strings.TrimSpace(m.Name)
		}
		if name == id {
			name = ""
		}
		out = append(out, VendorModel{ID: id, Name: name})
	}
	return out
}

var (
	vendorModelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	// familyRe splits an id at its first version: «kimi-k3.1-pro» → «kimi-k», «3.1», «-pro».
	familyRe   = regexp.MustCompile(`^(.*?)(\d+(?:\.\d+)*)(.*)$`)
	notForChat = regexp.MustCompile(`(?i)image|imagine|video|vision|embed|tts|audio|speech|voice|realtime|transcri|moderation|rerank|ocr`)
)

// splitVersion is an id's family, version and what follows the version; ok is false for an id without one.
func splitVersion(id string) (family string, version []int, suffix string, ok bool) {
	m := familyRe.FindStringSubmatch(strings.ToLower(id))
	if m == nil {
		return "", nil, "", false
	}
	for _, p := range strings.Split(m[2], ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", nil, "", false
		}
		version = append(version, n)
	}
	return m[1], version, m[3], true
}

func cmpVersion(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
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

func versionText(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// WithVendorModels adds to the preset the vendor's models that are newer versions of a family it has: each goes
// right before its newest sibling, with that sibling's window and efforts.
func WithVendorModels(p Preset, listed []VendorModel) Preset {
	if len(listed) == 0 || len(p.Models) == 0 {
		return p
	}
	models := slices.Clone(p.Models)
	for _, v := range listed {
		if notForChat.MatchString(v.ID) || slices.ContainsFunc(models, func(m Model) bool { return strings.EqualFold(m.ID, v.ID) }) {
			continue
		}
		fam, ver, suf, ok := splitVersion(v.ID)
		if !ok {
			continue
		}
		sib := -1
		var sibVer []int
		for i, m := range models {
			f, mv, s, ok := splitVersion(m.ID)
			if !ok || f != fam || s != suf {
				continue
			}
			if sib < 0 || cmpVersion(mv, sibVer) > 0 {
				sib, sibVer = i, mv
			}
		}
		if sib < 0 || cmpVersion(ver, sibVer) <= 0 {
			continue
		}
		s := models[sib]
		label := v.Name
		if label == "" {
			if old := versionText(sibVer); strings.Contains(s.Label, old) {
				label = strings.Replace(s.Label, old, versionText(ver), 1)
			} else {
				label = v.ID
			}
		}
		add := Model{ID: v.ID, Label: label, Window: s.Window, Efforts: slices.Clone(s.Efforts), DefaultEffort: s.DefaultEffort}
		models = slices.Insert(models, sib, add)
	}
	p.Models = models
	return p
}

// withVendorLists applies the kept lists to the presets (all()).
func withVendorLists(out []Preset) {
	vendorModels.RLock()
	defer vendorModels.RUnlock()
	for i := range out {
		if l := vendorModels.Lists[out[i].ID]; len(l) > 0 {
			out[i] = WithVendorModels(out[i], l)
		}
	}
}

// loadVendorModels reads the kept lists of dir once.
func loadVendorModels(dir string) {
	vendorModels.Lock()
	defer vendorModels.Unlock()
	file := filepath.Join(dir, "vendor-models.json")
	if vendorModels.loaded && vendorModels.file == file {
		return
	}
	vendorModels.file, vendorModels.loaded = file, true
	vendorModels.At, vendorModels.Lists = map[string]int64{}, map[string][]VendorModel{}
	if raw, err := os.ReadFile(file); err == nil {
		var doc struct {
			At    map[string]int64         `json:"at"`
			Lists map[string][]VendorModel `json:"lists"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			if doc.At != nil {
				vendorModels.At = doc.At
			}
			if doc.Lists != nil {
				vendorModels.Lists = doc.Lists
			}
		}
	}
}

func saveVendorModelsLocked() {
	raw, _ := json.Marshal(map[string]any{"at": vendorModels.At, "lists": vendorModels.Lists})
	if err := os.MkdirAll(filepath.Dir(vendorModels.file), 0o700); err != nil {
		return
	}
	tmp := vendorModels.file + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, vendorModels.file)
	}
}

// RefreshVendorModels asks each preset's vendor with a credential for its list, at most once in VendorModelsEvery
// (force: now), and keeps the lists in the store's folder. It says whether a list changed. A vendor that does not
// answer keeps what was kept.
func RefreshVendorModels(ctx context.Context, s *Store, client *http.Client, force bool) bool {
	if s == nil || s.Dir == "" {
		return false
	}
	loadVendorModels(s.Dir)
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	changed := false
	for _, p := range presets {
		u := vendorModelsURL[p.ID]
		if u == "" || !sameHost(u, p.BaseURL) {
			continue
		}
		vendorModels.RLock()
		at := vendorModels.At[p.ID]
		vendorModels.RUnlock()
		if !force && time.Since(time.Unix(at, 0)) < VendorModelsEvery {
			continue
		}
		key, err := s.Credential(ctx, p.ID)
		if err != nil || key == "" {
			continue
		}
		list, ok := fetchVendorModels(ctx, client, u, key)
		vendorModels.Lock()
		vendorModels.At[p.ID] = time.Now().Unix()
		if ok && len(list) > 0 && !slices.Equal(list, vendorModels.Lists[p.ID]) {
			vendorModels.Lists[p.ID] = list
			changed = true
		}
		saveVendorModelsLocked()
		vendorModels.Unlock()
	}
	return changed
}

func sameHost(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	return err == nil && ua.Scheme == "https" && strings.EqualFold(ua.Host, ub.Host)
}

func fetchVendorModels(ctx context.Context, client *http.Client, u, key string) ([]VendorModel, bool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("x-api-key", key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return nil, false
	}
	return ParseVendorModels(raw), true
}
