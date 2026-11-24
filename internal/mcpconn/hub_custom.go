package mcpconn

// connection_add: the way an agent connects a service of the person's own. It assembles the record, tries
// the service (an MCP address's initialize and tools/list, an API's check path), and then asks the person on a card in
// the chat: nothing is made on the agent's word. Keys are never an argument: the agent names a secret of the safe
// (asked for with `harnessly secret ask`), the runtime copies it in.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// ConfirmRequest is what the hub asks the person to agree to.
type ConfirmRequest struct {
	// ProposalID keeps waiting on an earlier request (the agent asked again); the rest is then ignored.
	ProposalID string
	Title      string
	TitleRu    string
	Name       string
	Pairs      []Pair
	Why        string
	// Run makes it, once, after the person's yes.
	Run func(ctx context.Context) (string, error)
}

// ConfirmAnswer is how the person answered: State is waiting, done, failed, declined or expired.
type ConfirmAnswer struct {
	ID     string
	State  string
	Result string
}

// Confirmer shows the request as a card in the chat of the agent that called (the runtime knows which) and waits at
// most wait for the answer.
type Confirmer func(ctx context.Context, req ConfirmRequest, wait time.Duration) (ConfirmAnswer, error)

// confirmWait is how long one connection_add call waits for the person; the agent calls again to keep waiting.
const confirmWait = 90 * time.Second

func (h *Hub) confirmWait() time.Duration {
	if h.ConfirmWait > 0 {
		return h.ConfirmWait
	}
	return confirmWait
}

var connectionAddTool = map[string]any{"name": "connection_add",
	"description": "Connect a service that is not in the catalog (the person's own API, a company's internal service, a self-made or third-party MCP server) or change one you made (id). Give what you found out from its address, docs or OpenAPI file: kind mcp (an MCP server's address), api (a base address, where the keys go, a check path, a guide of the main paths for agents) or local (npx/uvx at an exact version: someone else's code, runs on this machine). The service is tried first (MCP: initialize and tools/list; API: the check path), then the person confirms on a card in this chat and only then it is made; it then works like any connection (connections, search_tools, call, api_request, and the person's apps). Never put a key or token in an argument: ask for it first with `harnessly secret ask <id>-<key-name> --why …` in a shell and name that secret in keys[].safe. dry_run only tries and shows what would be made.",
	"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
		"help":       map[string]any{"type": "boolean", "description": "true (alone): the step-by-step guide to connecting a service of one's own; read it before the first one"},
		"id":         map[string]any{"type": "string", "description": "custom-<slug> (lowercase letters, digits, dashes); empty makes it of the name; an existing id changes that service"},
		"kind":       map[string]any{"type": "string", "enum": []string{"mcp", "api", "local"}},
		"name":       map[string]any{"type": "string", "description": "the person's name for it"},
		"url":        map[string]any{"type": "string", "description": "mcp: the server's address; api: the base address without a query (it may hold {KEY}, e.g. https://{HOST}/api)"},
		"transport":  map[string]any{"type": "string", "enum": []string{"http", "sse"}, "description": "mcp: sse only for a legacy HTTP+SSE server; the probe decides"},
		"headerName": map[string]any{"type": "string", "description": "mcp: the header the key goes in (default Authorization; a bare token gets Bearer)"},
		"keys": map[string]any{"type": "array", "description": "api and local: the keys and settings the service takes; mcp with a header: one key. Values are never given here",
			"items": map[string]any{"type": "object", "required": []string{"name"}, "properties": map[string]any{
				"name":     map[string]any{"type": "string", "description": "A-Z, 0-9, _ — the name the templates and the process know it by"},
				"label":    map[string]any{"type": "string", "description": "what the person calls it"},
				"hint":     map[string]any{"type": "string", "description": "where to get it"},
				"plain":    map[string]any{"type": "boolean", "description": "a setting that is not a secret (a host, a region)"},
				"optional": map[string]any{"type": "boolean"},
				"safe":     map[string]any{"type": "string", "description": "the secret of the safe that holds it, named <id>-…; ask for it first with `harnessly secret ask`"}}}},
		"apiHeaders": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "api: headers carrying keys, \"{KEY}\" in the value, e.g. {\"Authorization\":\"Bearer {TOKEN}\"}; {BASIC:LOGIN:PASSWORD} for basic auth"},
		"apiQuery":   map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "api: query parameters carrying keys, e.g. {\"api_key\":\"{KEY}\"}"},
		"checkPath":  map[string]any{"type": "string", "description": "api: a GET path that answers 2xx once the keys are right (/me, /health)"},
		"guide":      map[string]any{"type": "string", "description": "api: the main paths and what they do, a few lines for the agents that use it later (made from the OpenAPI file)"},
		"command":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "local: [\"npx\",\"-y\",\"pkg@1.2.3\",…] or [\"uvx\",\"pkg==1.2.3\",…]; the version exact"},
		"category":   map[string]any{"type": "string", "description": "the group: mail, calendar, chat, tasks, docs, files, code, monitoring, support, crm, payments, finance, sites, cloud, design, media, search, research, seo, utility"},
		"letter":     map[string]any{"type": "string", "description": "one character for its mark"},
		"about":      map[string]any{"type": "string", "description": "what the service is, a few sentences, for the person"},
		"aboutRu":    map[string]any{"type": "string"},
		"can":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "three to five things an agent can do with it, short phrases"},
		"canRu":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"steps":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "the steps to get the key or switch the access on"},
		"stepsRu":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"links": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
			"kind": map[string]any{"type": "string", "enum": []string{"docs", "setup", "token", "pricing", "home"}}, "url": map[string]any{"type": "string"}}}},
		"why":      map[string]any{"type": "string", "description": "one line for the card: what the person asked for"},
		"dry_run":  map[string]any{"type": "boolean", "description": "only try it and say what would be made"},
		"proposal": map[string]any{"type": "string", "description": "the id a call that said \"waiting\" gave: keep waiting for the person's yes, nothing else needed"}}}}

// connectionAdd is the tool.
func (h *Hub) connectionAdd(ctx context.Context, raw json.RawMessage) map[string]any {
	if h.Confirm == nil {
		return textResult("connection_add is not available here", true)
	}
	var a struct {
		CustomInput
		Why      string `json:"why"`
		Help     bool   `json:"help"`
		KeysOnly bool   `json:"keys_only"`
		DryRun   bool   `json:"dry_run"`
		Proposal string `json:"proposal"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return textResult("the arguments are not what connection_add takes: "+err.Error(), true)
	}
	if a.Help {
		return textResult(customGuide, false)
	}
	if a.Proposal != "" {
		ans, err := h.Confirm(ctx, ConfirmRequest{ProposalID: a.Proposal}, h.confirmWait())
		if err != nil {
			return textResult("that request is gone (answered, or out of time): call connection_add again", true)
		}
		return h.answered(ans)
	}
	if a.KeysOnly {
		srv, err := h.S.AttachSafeKeys(ctx, a.ID, a.Keys)
		if err != nil {
			return textResult(planWhy(err), true)
		}
		return textResult("Keys given. "+madeLine(srv), srv.State != StateOK)
	}
	plan, err := h.S.PlanCustom(ctx, a.CustomInput, nil)
	if err != nil {
		return textResult(planWhy(err), true)
	}
	summary := plan.summary()
	if a.DryRun {
		return textResult("Would make (nothing was made):\n"+summary, false)
	}
	in := plan.In
	title, titleRu := "Connect a service of your own", "Подключить свой сервис"
	if plan.Existing != nil {
		title, titleRu = "Change a service of your own", "Изменить свой сервис"
	}
	req := ConfirmRequest{Title: title, TitleRu: titleRu, Name: in.Name, Pairs: plan.Pairs, Why: a.Why,
		Run: func(ctx context.Context) (string, error) {
			srv, _, err := h.S.SaveCustom(ctx, in, nil)
			if err != nil {
				return "", err
			}
			return madeLine(srv), nil
		}}
	ans, err := h.Confirm(ctx, req, h.confirmWait())
	if err != nil {
		return textResult("could not ask the person: "+err.Error(), true)
	}
	return h.answered(ans)
}

func (h *Hub) answered(ans ConfirmAnswer) map[string]any {
	switch ans.State {
	case "done":
		return textResult(ans.Result, false)
	case "declined":
		return textResult("The person declined. Nothing was made. Ask what they want changed; do not propose it again unchanged.", true)
	case "expired":
		return textResult("The person did not answer in time. Nothing was made; propose it again when they are back.", true)
	case "failed":
		return textResult("The person agreed but it did not work: "+ans.Result, true)
	}
	return textResult(fmt.Sprintf("waiting: the person has a card in this chat. Call connection_add again with proposal=%q to keep waiting (no other arguments).", ans.ID), false)
}

// madeLine tells an agent what the yes made and what is left to do.
func madeLine(srv Server) string {
	line := fmt.Sprintf("Made %s as connection %q (%s, state %s, %d tools).", srv.CatalogID, srv.Slug, kindOf(srv), srv.State, len(srv.Tools))
	switch {
	case srv.State == StateSignIn && srv.Auth == AuthOAuth:
		line += " It signs in with OAuth: tell the person to open Settings → Connections → " + srv.Name + " and press Sign in."
	case srv.State == StateSignIn:
		line += " It waits for its keys: for each, `harnessly secret ask " + srv.CatalogID + "-<key-name> --why …`, then call connection_add again with id " + srv.CatalogID + " and keys[].safe naming them; or the person types them on its page in Settings → Connections."
	case srv.State != StateOK:
		line += " It does not answer yet" + strings.TrimSpace(" ("+srv.Problem+")") + ": manage(check) after the person fixes it."
	case srv.Transport == API:
		line += " Use api_request(connection: \"" + srv.Slug + "\", path)."
	default:
		line += " Use search_tools or connections(\"" + srv.Slug + "\"), then call."
		line += recipeHint
	}
	return line
}

// recipeHint is the step an MCP service without events is offered (docs/connectors.md «MCP recipe»): its
// updates reach the digest by a poll recipe.
const recipeHint = " If the person wants its updates (new results, messages, statuses) in their digest or notices, offer a poll recipe: guide step 9."

// planWhy is a failed plan in words an agent can act on (never the service's own words: they may carry a key).
func planWhy(err error) string {
	switch {
	case errors.Is(err, ErrBadRequest):
		return strings.TrimPrefix(err.Error(), ErrBadRequest.Error()+": ")
	case errors.Is(err, ErrDenied):
		return err.Error() + ". Ask the person for a right key (`harnessly secret ask … --replace`)."
	case errors.Is(err, ErrUnreachable):
		return strings.TrimPrefix(err.Error(), ErrUnreachable.Error()+": ") + ". Check the address with the person or the docs."
	}
	return "could not try the service: " + err.Error()
}

func (p CustomPlan) summary() string {
	var b strings.Builder
	for _, x := range p.Pairs {
		fmt.Fprintf(&b, "- %s: %s\n", x.Name, x.Value)
	}
	if len(p.In.Keys) > 0 && slices.ContainsFunc(p.In.Keys, func(k CustomKey) bool { return k.Safe == "" }) {
		b.WriteString(locale.Pick("Keys without a safe secret are typed by the person on the service's page.\n", "Ключи без секрета в сейфе человек введёт на странице сервиса.\n"))
	}
	return b.String()
}

// customGuide is the step-by-step way for an agent to connect a service of the person's own (connection_add help).
const customGuide = `Connecting a service of the person's own, step by step

1. Find out what it is. An address that answers MCP (…/mcp, …/sse, an "MCP server" in its docs) is kind "mcp". A REST API with a base address and an OpenAPI/Swagger file (openapi.json, swagger.json, /docs) or a docs page is kind "api". An npm or PyPI package the person names, to run on this machine, is kind "local": only npx -y <package>@<exact version> or uvx <package>==<exact version>, and only when the person asked for that package; never a script, a Docker image or "latest". Read the spec or the docs yourself (the spec itself needs no key).
2. For an API take from the spec: the base address (servers[0].url, without a query), where the key goes (security scheme: apiKey in a header -> apiHeaders {"<Header>": "{API_KEY}"}; http bearer -> {"Authorization": "Bearer {API_KEY}"}; basic -> keys USER and PASSWORD and {"Authorization": "{BASIC:USER:PASSWORD}"}; apiKey in the query -> apiQuery {"<name>": "{API_KEY}"}), a checkPath (a cheap GET without parameters that answers 2xx with a right key: /me, /health, /v1/ping), and a guide of 5-12 lines for the agents that will use it: "METHOD /path - what it does (the parameters that matter)", the main paths only, with the paging and the limits if the spec says. Hosts that differ per customer: {HOST} in url and a key {HOST} with plain: true.
3. Write the description in the person's language (and aboutRu when they write Russian): about (two or three sentences: what the service is and for whom), can (three to five short things an agent can do), category (one of the groups), letter, steps (how to get a key, if the docs say), links (docs).
4. Keys. Never type a key into an argument, a command line or the chat, and never put one into apiHeaders (only "{KEY}" placeholders). First look where the team keeps it (its skills, the vault): ask the person only when it is nowhere. For each key run in a shell, IN THE BACKGROUND (run_in_background: the person may take minutes, do not wait on it and do not stop your work for it): harnessly secret ask <id>-<key-name-lowercase> --why "what it is for" (the id is custom-<slug>, e.g. custom-acme-orders-api-key). The secret's name goes into keys[].safe.
5. Try it: connection_add with dry_run true (before or while the key is asked). It probes an MCP address (initialize and tools/list) and asks an API's check path when the key is already in the safe, and says what would be made. Fix what it says (a wrong address, a refused key) before the person is asked anything.
6. Ask: connection_add with the same arguments and why (what the person asked for), without waiting for the key. The person gets a card in this chat with everything that will be made and confirms; nothing exists before that. "waiting": call connection_add again with proposal=<the id> and nothing else (the person may be away); "declined": ask what to change, do not propose the same again. A key that is not in the safe yet leaves the service waiting for it.
6b. When a secret ask says "ready" for a service that was made without its key: connection_add with id, keys_only true and keys[] naming the secrets (no card: the person agreed to the service already). It copies the values, tries them and says whether the service works.
7. After "Made …": the service is a connection like any other. Look at it with connections(query) or manage(check), use it with call / api_request. An app names it in its manifest as connections: [{"id": "custom-<slug>"}].
8. To change a service you made, give its id again with the new arguments (same card); a key you do not name stays as it is. To remove one the person uses Settings → Connections.
9. Updates of an MCP service into the digest (it has no webhooks, only tools). Find a tool that lists things (jobs, messages, records) and only reads: tool_schema shows its annotations; one not marked readOnlyHint the person must allow, one marked destructiveHint never. Call it once to see the answer's shape. Write a recipe: {"trigger": "<what happened, e.g. generation.done>", "tool": "<tool>", "args": {… "{{cursor}}" where it takes a since/after …}, "items": "<dotted path to the list>", "id": "<field>", "at": "<time field>", "title": "<field>", "url": "<field>", "cursor": "<time or id field>", "every": "10m"}. Never a tool that creates, sends or spends. Give the person the recipe and the way: Settings → Connections → Feeds → «MCP recipe», server <slug>, recipe <the JSON>; its check runs the recipe once and shows what it found. From then on the machine polls by itself and every new thing is an event of the journal and the digest (docs/connectors.md, «MCP recipe»).
`
