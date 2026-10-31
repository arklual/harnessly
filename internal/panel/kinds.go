package panel

// The kinds of tab the right pane holds: one registry, which the agent's command parser, `harnessly open` and the
// page's pane (web/app/lib/pane-kinds.ts keeps the same names) all read. An installed app adds no kind of its own: it
// opens in the "app" kind, whose target is "<app id>[/<view>]", so a new app needs no change to this list.

type kind struct {
	name   string
	target targetKind
}

var registry = []kind{
	{"overview", none}, {"tools", none}, {"browser", port}, {"terminal", none}, {"files", path}, {"disk", none},
	{"changes", path}, {"artifacts", doc}, {"gitlab", gitlab}, {"subagents", none}, {"economics", none}, {"plan", none},
	{"peers", none}, {"design", designRef}, {"side-chat", none}, {"gallery", none}, {"task", task}, {"app", appRef}, {"chrome", none},
}

// Kinds are the kinds of tab the pane can hold, in the order the pane lists them.
func Kinds() []string {
	out := make([]string, len(registry))
	for i, k := range registry {
		out[i] = k.name
	}
	return out
}

// TabKinds are Kinds, for the callers that range over them.
var TabKinds = Kinds()

func targetOfKind(name string) targetKind {
	for _, k := range registry {
		if k.name == name {
			return k.target
		}
	}
	return none
}
