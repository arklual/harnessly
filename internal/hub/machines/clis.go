package machines

import (
	"encoding/json"
	"regexp"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The statuses of a machine's other CLIs to sign in (#58). The machine describes them in its cli-logins.json and the
// hub knows of them only from what it reports; what it reports is cut to a short, plain shape.

// CLIID is what a CLI's id may be: the agent refuses others, and so does the hub.
var CLIID = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

const (
	maxCLIs     = 16
	maxCLILabel = 60
)

// IsCLIID says id may name a CLI: the right shape and not an agent.
func IsCLIID(id string) bool {
	return CLIID.MatchString(id) && id != hubproto.ProviderClaude && id != hubproto.ProviderCodex && id != hubproto.ProviderGemini
}

// cliStatus is a reported CLI status as it is kept: id, label, installed and login, nothing else.
func cliStatus(a hubproto.AgentStatus) (json.RawMessage, bool) {
	if !IsCLIID(a.Provider) {
		return nil, false
	}
	label := clamp(a.Label, maxCLILabel)
	if label == "" {
		label = a.Provider
	}
	login := a.Login
	if login != hubproto.LoggedIn && login != hubproto.LoggedOut {
		login = hubproto.LoginUnknown
	}
	raw, err := json.Marshal(map[string]any{"provider": a.Provider, "kind": hubproto.KindCLI, "label": label,
		"installed": a.Installed, "login": login})
	return raw, err == nil
}

// mergeCLIs is the kept list after a report: the new list when the report was full, else the old one with the
// reported statuses put in place.
func mergeCLIs(old json.RawMessage, fresh map[string]json.RawMessage, order []string, full bool) json.RawMessage {
	var list []json.RawMessage
	if full {
		for _, id := range order {
			list = append(list, fresh[id])
		}
	} else {
		_ = json.Unmarshal(old, &list)
		for i, raw := range list {
			var c struct{ Provider string }
			if json.Unmarshal(raw, &c) == nil && fresh[c.Provider] != nil {
				list[i] = fresh[c.Provider]
			}
		}
	}
	if len(list) == 0 {
		return nil
	}
	out, _ := json.Marshal(list)
	return out
}
