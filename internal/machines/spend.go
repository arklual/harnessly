package machines

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// What Claude cost on this machine lately, for a person who pays Claude by the API rather than by a subscription: the
// API-equivalent cost of every call in the transcripts, summed by the hour and by the model's family. The page sums the
// hours since the start of its day (an hour of the person's choosing), so the snapshot stays one for every page.

const (
	// spendHours: the start of a day is at most a day back; one more covers an hour the clock is still in.
	spendHours = 25
	// Billing modes of Claude Code on the machine.
	BillingSubscription = "subscription"
	BillingAPI          = "api"
)

// modelFamilies name a model by its family; another model goes by its own id.
var modelFamilies = []string{"opus", "sonnet", "haiku", "fable"}

func modelFamily(model string) string {
	m := strings.ToLower(model)
	for _, f := range modelFamilies {
		if strings.Contains(m, f) {
			return f
		}
	}
	return model
}

// SpendHour is the cost of one hour, USD by the model family; Start is the hour's first epoch second (UTC).
type SpendHour struct {
	Start   float64            `json:"start"`
	ByModel map[string]float64 `json:"byModel"`
}

// Spend is Claude's cost by the hour over the last day, oldest first; hours that cost nothing are left out.
type Spend struct {
	Hours []SpendHour `json:"hours"`
	// AsOf: the transcripts were read up to this epoch second.
	AsOf float64 `json:"asOf"`
	// Billing is how Claude Code is paid on the machine (BillingSubscription, BillingAPI); nil when it cannot be told.
	Billing *string `json:"billing"`
}

// SpendTotal is what was spent since a moment.
type SpendTotal struct {
	USD     float64
	ByModel map[string]float64
}

// Since sums the hours that start at or after from (epoch seconds; the page's day starts on the hour).
func (s *Spend) Since(from float64) SpendTotal {
	out := SpendTotal{ByModel: map[string]float64{}}
	for _, h := range s.Hours {
		if h.Start < from {
			continue
		}
		for m, usd := range h.ByModel {
			out.ByModel[m] += usd
			out.USD += usd
		}
	}
	return out
}

type spendBuilder struct {
	from, asOf float64
	hours      map[float64]map[string]float64
}

func newSpendHours(asOf float64) *spendBuilder {
	from := math.Floor(asOf/hour)*hour - (spendHours-1)*hour
	return &spendBuilder{from: from, asOf: asOf, hours: map[float64]map[string]float64{}}
}

func (b *spendBuilder) add(ts float64, model string, usd float64) {
	if ts < b.from || ts > b.asOf || !(usd > 0) {
		return
	}
	start := math.Floor(ts/hour) * hour
	if b.hours[start] == nil {
		b.hours[start] = map[string]float64{}
	}
	b.hours[start][modelFamily(model)] += usd
}

func (b *spendBuilder) done() *Spend {
	out := &Spend{Hours: make([]SpendHour, 0, len(b.hours)), AsOf: b.asOf}
	for start, by := range b.hours {
		out.Hours = append(out.Hours, SpendHour{Start: start, ByModel: by})
	}
	sort.Slice(out.Hours, func(i, j int) bool { return out.Hours[i].Start < out.Hours[j].Start })
	return out
}

// Spend is Claude's cost by the hour as the last pass over the transcripts read it (Activity starts the passes); nil
// before the first one.
func (u *Usage) Spend() *Spend {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.spend
}

// ClaudeBilling tells from Claude Code's own files how it is paid: a claude.ai login that may run models is a
// subscription; else a key the console login left in the config is the API. The files are read again only when they
// change.
type ClaudeBilling struct {
	// ConfigDir is Claude Code's folder (CLAUDE_CONFIG_DIR, else ~/.claude); ConfigFile its .claude.json.
	ConfigDir, ConfigFile string

	mu    sync.Mutex
	stamp [2]fileStamp
	mode  *string
}

type fileStamp struct {
	size  int64
	mtime time.Time
	ok    bool
}

func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{st.Size(), st.ModTime(), true}
}

// Mode is BillingSubscription, BillingAPI or nil.
func (c *ClaudeBilling) Mode() *string {
	creds := filepath.Join(c.ConfigDir, ".credentials.json")
	stamp := [2]fileStamp{stampOf(creds), stampOf(c.ConfigFile)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if stamp == c.stamp && (stamp[0].ok || stamp[1].ok) {
		return c.mode
	}
	c.stamp, c.mode = stamp, billingOf(creds, c.ConfigFile)
	return c.mode
}

func billingOf(credsFile, configFile string) *string {
	var creds struct {
		ClaudeAiOauth *struct {
			Scopes           []string `json:"scopes"`
			SubscriptionType *string  `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if raw, err := os.ReadFile(credsFile); err == nil && json.Unmarshal(raw, &creds) == nil && creds.ClaudeAiOauth != nil {
		o := creds.ClaudeAiOauth
		if (o.SubscriptionType != nil && *o.SubscriptionType != "") || slices.Contains(o.Scopes, "user:inference") {
			mode := BillingSubscription
			return &mode
		}
	}
	var config struct {
		PrimaryAPIKey string `json:"primaryApiKey"`
	}
	if raw, err := os.ReadFile(configFile); err == nil && json.Unmarshal(raw, &config) == nil && config.PrimaryAPIKey != "" {
		mode := BillingAPI
		return &mode
	}
	return nil
}
