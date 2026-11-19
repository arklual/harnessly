package machines

import (
	"context"
	"encoding/json"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Setup is what the machine's card shows beyond the View: how much of the environment the machine said it holds and
// the accounts its agents work on.
type Setup struct {
	// Env is nil while the machine has not reported its environment.
	Env      *EnvSetup      `json:"env"`
	Accounts []AccountSetup `json:"accounts"`
}

// EnvSetup counts the items the machine reported: Applied of them are in place, Total were reported.
type EnvSetup struct {
	Applied  int     `json:"applied"`
	Total    int     `json:"total"`
	ReportAt *string `json:"reportedAt"`
}

// AccountSetup is the account a machine's agent of one provider last worked on.
type AccountSetup struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Plan     string `json:"plan"`
}

// setup reads the Setup of a machine; each part that cannot be known stays empty.
func (s *Service) setup(ctx context.Context, m store.Machine) (Setup, error) {
	out := Setup{Accounts: []AccountSetup{}}
	st := s.db.S()
	es, err := st.EnvStateByMachine(ctx, m.ID)
	switch {
	case store.IsNotFound(err):
	case err != nil:
		return out, err
	case !es.At.IsZero():
		var applied []hubproto.EnvApplied
		if json.Unmarshal([]byte(es.Applied), &applied) == nil {
			e := &EnvSetup{Total: len(applied)}
			for _, a := range applied {
				if a.Status == hubproto.EnvOK {
					e.Applied++
				}
			}
			at := es.At.UTC().Format("2006-01-02T15:04:05Z")
			e.ReportAt = &at
			out.Env = e
		}
	}
	uses, err := st.MachineAccountUses(ctx, m.ID)
	if err != nil {
		return out, err
	}
	ids := make([]string, len(uses))
	for i, u := range uses {
		ids[i] = u.AccountID
	}
	accs, err := st.AccountsByIDs(ctx, ids)
	if err != nil {
		return out, err
	}
	for _, u := range uses {
		if a, ok := accs[u.AccountID]; ok {
			out.Accounts = append(out.Accounts, AccountSetup{Provider: u.Provider, Label: a.Label, Kind: a.Kind, Plan: a.Plan})
		}
	}
	return out, nil
}
