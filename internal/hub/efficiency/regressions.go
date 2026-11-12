package efficiency

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

// Regression is one alert of GET .../efficiency/regressions: a metric that left its band after a change of the harness.
type Regression struct {
	ID              string   `json:"id"`
	BatchID         string   `json:"batchId"`
	Metric          string   `json:"metric"`
	Baseline        float64  `json:"baseline"`
	Observed        float64  `json:"observed"`
	Band            int      `json:"band"`
	FingerprintFrom string   `json:"fingerprintFrom"`
	FingerprintTo   string   `json:"fingerprintTo"`
	Diff            []string `json:"diff"`
	State           string   `json:"state"`
	SeenAt          *string  `json:"seenAt"`
	CreatedAt       string   `json:"createdAt"`
}

// regressionIn is one alert of an import. The names are those of the old gateway's table.
type regressionIn struct {
	ID              string   `json:"id"`
	BatchID         string   `json:"batch_id"`
	Metric          string   `json:"metric"`
	Baseline        float64  `json:"baseline"`
	Observed        float64  `json:"observed"`
	Band            int      `json:"band"`
	FingerprintFrom string   `json:"fingerprint_from"`
	FingerprintTo   string   `json:"fingerprint_to"`
	Diff            []string `json:"diff"`
	Addressee       string   `json:"addressee"`
	SeenAt          string   `json:"seen_at"`
	State           string   `json:"state"`
	ResolvedBy      string   `json:"resolved_by"`
	CreatedAt       string   `json:"created_at"`
}

const maxRegressions = 500

// ImportRegressions stores the alerts of raw, a JSON array of {id, batch_id?, metric, baseline, observed, band,
// fingerprint_from?, fingerprint_to?, diff?, addressee, seen_at?, state?, resolved_by?, created_at (RFC 3339)}, and says how
// many it read. The instance's own alerts come this way (HUB_EFFICIENCY_REGRESSIONS); every start reads them again and an
// alert already stored is left as it is.
func ImportRegressions(ctx context.Context, st store.Store, raw string) (int, error) {
	var in []regressionIn
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return 0, fmt.Errorf("regressions: %w", err)
	}
	if len(in) > maxRegressions {
		return 0, fmt.Errorf("regressions: at most %d", maxRegressions)
	}
	rows := make([]store.EffRegressionAlert, 0, len(in))
	for i, a := range in {
		created, err := time.Parse(time.RFC3339, a.CreatedAt)
		if err != nil {
			return 0, fmt.Errorf("regressions[%d]: created_at must be RFC 3339", i)
		}
		state := a.State
		if state == "" {
			state = "open"
		}
		if a.ID == "" || strings.TrimSpace(a.Metric) == "" || len(a.Metric) > 300 || (state != "open" && state != "resolved") || a.Band < 0 || a.Band > 9 {
			return 0, fmt.Errorf("regressions[%d]: bad id, metric, band or state", i)
		}
		if a.Diff == nil {
			a.Diff = []string{}
		}
		diff, _ := json.Marshal(a.Diff)
		row := store.EffRegressionAlert{ID: a.ID, BatchID: a.BatchID, Metric: a.Metric, Baseline: a.Baseline, Observed: a.Observed,
			Band: a.Band, FingerprintFrom: a.FingerprintFrom, FingerprintTo: a.FingerprintTo, Diff: string(diff),
			Addressee: strings.ToLower(strings.TrimSpace(a.Addressee)), State: state, ResolvedBy: a.ResolvedBy, CreatedAt: created.UnixMilli()}
		if a.SeenAt != "" {
			seen, err := time.Parse(time.RFC3339, a.SeenAt)
			if err != nil {
				return 0, fmt.Errorf("regressions[%d]: seen_at must be RFC 3339", i)
			}
			ms := seen.UnixMilli()
			row.SeenAt = &ms
		}
		rows = append(rows, row)
	}
	if err := st.InsertEffRegressionAlerts(ctx, rows); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// regressions answers the viewer's own alerts, open ones by default (?state=all adds the resolved). Nobody reads another
// person's alerts.
func (s *Service) regressions(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	if err := s.gate(r, t); err != nil {
		return err
	}
	rows, err := s.DB.S().EffRegressionAlertsFor(r.Context(), strings.ToLower(t.Person.User.Email), r.URL.Query().Get("state") != "all")
	if err != nil {
		return err
	}
	out := make([]Regression, 0, len(rows))
	for _, a := range rows {
		g := Regression{ID: a.ID, BatchID: a.BatchID, Metric: a.Metric, Baseline: a.Baseline, Observed: a.Observed, Band: a.Band,
			FingerprintFrom: a.FingerprintFrom, FingerprintTo: a.FingerprintTo, State: a.State,
			CreatedAt: time.UnixMilli(a.CreatedAt).UTC().Format(time.RFC3339)}
		if json.Unmarshal([]byte(a.Diff), &g.Diff) != nil || g.Diff == nil {
			g.Diff = []string{}
		}
		if a.SeenAt != nil {
			v := time.UnixMilli(*a.SeenAt).UTC().Format(time.RFC3339)
			g.SeenAt = &v
		}
		out = append(out, g)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"alerts": out})
	return nil
}
