package apps

import "sort"

// Surface is the one description of what an app reaches in the host: the places it works in, the rights it
// asks for, the methods of the bridge and what each needs, the host tools of the agent. The manifest check, the bridge
// and the catalog read it here; the page's table (web/app/next/features/app-views/surface.gen.ts), the reference
// (docs/apps/reference.md) and the JSON Schema of app.json (docs/apps/app.schema.json) are written from it by
// `UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent`, and TestSurfaceFilesAreCurrent fails the build when one of them was edited by hand.

// SurfaceTarget is a place an app works in.
type SurfaceTarget struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Slot: a place of the interface filled by a view at a spot the page declares.
	Slot bool `json:"slot,omitempty"`
	// Pattern: the name stands for a family (sinopia.<kind>).
	Pattern bool `json:"pattern,omitempty"`
}

// SurfacePermission is a right an app asks for in `permissions`.
type SurfacePermission struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Param is what follows the right after ':' (files.read:<mask>); "" is a right written alone.
	Param string `json:"param,omitempty"`
	// Unsupported: the host accepts the right in no call yet, so the check refuses it.
	Unsupported bool `json:"unsupported,omitempty"`
}

// SurfaceMethod is a method of the host↔view bridge.
type SurfaceMethod struct {
	Name string `json:"name"`
	// Permission is the right the method needs; "" needs none.
	Permission string `json:"permission"`
	// SDK is how a view calls it through @harnessly/app-sdk.
	SDK string `json:"sdk,omitempty"`
	Doc string `json:"doc"`
}

// SurfaceTool is a host tool the agent of a chat with apps gets from the harnessly-app server.
type SurfaceTool struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
}

var surfaceTargets = []SurfaceTarget{
	{Name: TargetChat, Doc: "The chat: the agent profile, /-commands of the palette, cards and widgets in the feed."},
	{Name: TargetPane, Doc: "A tab of the right pane beside the chat."},
	{Name: TargetApp, Doc: "A full screen of its own, opened from the apps page or by link."},
	{Name: TargetSettings, Doc: "A page in the settings.", Slot: true},
	{Name: TargetOnboarding, Doc: "A step of the first run.", Slot: true},
	{Name: TargetObjectAction, Doc: "A button at an object (a machine, a chat) that opens the view.", Slot: true},
	{Name: TargetHubSettings, Doc: "A page in the settings of a Hub.", Slot: true},
	{Name: TargetBranding, Doc: "The product's name and accent; declared values, no view.", Slot: true},
	{Name: "sinopia.<kind>", Doc: "The editor of one Sinopia document type: panels, selection items, checks, bindings.", Pattern: true},
	{Name: "sinopia.*", Doc: "The editor of every Sinopia document type.", Pattern: true},
}

var surfacePermissions = []SurfacePermission{
	{Name: "chat.prompt", Doc: "Send a message to the agent of the chat (hl.chat.send)."},
	{Name: "chat.read", Doc: "Read the chat's messages."},
	{Name: "chat.card", Doc: "Put a card of the app in the chat's feed."},
	{Name: "agents.read", Doc: "List the agents of the workspace."},
	{Name: "pane.open", Doc: "Open a view of the app in the right pane."},
	{Name: "storage", Doc: "Keep data of the app per chat, per document and per person."},
	{Name: "storage.space.write", Doc: "Write the storage area shared by the whole space."},
	{Name: "files.read", Param: "<mask>", Doc: "Read workspace files matching the mask."},
	{Name: "files.write", Param: "<mask>", Doc: "Write workspace files matching the mask."},
	{Name: "secrets", Param: "<name>", Doc: "Have the machine put a secret the person typed into the app's server calls."},
	{Name: "notify", Doc: "Show the person a notification."},
	{Name: "tools.page", Doc: "Offer tools from the view's page that the agent of the chat calls (hl.tools.register, navigator.modelContext.registerTool)."},
	{Name: "context.publish", Doc: "Publish what the view shows for the agent of the chat to read (hl.publish)."},
	{Name: "sinopia.read", Doc: "Read the Sinopia document the view is shown in."},
	{Name: "sinopia.write", Doc: "Change the Sinopia document: patches, fields, new boards."},
	{Name: "sinopia.comment", Doc: "Comment on the Sinopia document."},
	{Name: "sinopia.kind", Doc: "Add Sinopia document types of the app's own."},
	{Name: "sinopia.export", Doc: "Render the document to a file.", Unsupported: true},
	{Name: "ai", Doc: "Call a model on the person's behalf.", Unsupported: true},
	{Name: "users.read", Doc: "Read the people of the space.", Unsupported: true},
}

var surfaceMethods = []SurfaceMethod{
	{Name: "ui/initialize", Doc: "The view's handshake: the host answers its context."},
	{Name: "ui/notifications/initialized", Doc: "The view says it is ready."},
	{Name: "harnessly/debug.log", Doc: "An error or console.error of the view (the SDK sends it), for the debug section of the app's inspector."},
	{Name: "ui/notifications/size-changed", SDK: "hl.resize(height)", Doc: "The view asks for a height."},
	{Name: "ui/open-link", Doc: "Open an address in a new tab."},
	{Name: "tools/call", SDK: "hl.tools.call(name, args)", Doc: "Call a tool of the app's own MCP server."},
	{Name: "ui/message", Permission: "chat.prompt", SDK: "hl.chat.send(text)", Doc: "A message to the agent of this chat, signed with the app's name."},
	{Name: "harnessly/chat.read", Permission: "chat.read", SDK: "hl.chat.read({last})", Doc: "The chat's last messages."},
	{Name: "harnessly/chat.card", Permission: "chat.card", SDK: "hl.chat.card(view, data)", Doc: "A card of the app in the feed."},
	{Name: "harnessly/pane.open", Permission: "pane.open", SDK: "hl.pane.open(target)", Doc: "Open a view of the app in the right pane."},
	{Name: "harnessly/agents.list", Permission: "agents.read", SDK: "hl.agents.list()", Doc: "The agents of the workspace."},
	{Name: "harnessly/storage.get", Permission: "storage", SDK: "hl.storage.<scope>.get(key)", Doc: "Read a key of the app's storage."},
	{Name: "harnessly/storage.set", Permission: "storage", SDK: "hl.storage.<scope>.set(key, value, {ifVersion})", Doc: "Write a key; ifVersion writes only over that version."},
	{Name: "harnessly/storage.delete", Permission: "storage", SDK: "hl.storage.<scope>.delete(key)", Doc: "Remove a key."},
	{Name: "harnessly/storage.keys", Permission: "storage", SDK: "hl.storage.<scope>.keys()", Doc: "The keys of an area."},
	{Name: "harnessly/settings.get", SDK: "hl.settings.get()", Doc: "The app's settings as the person saved them."},
	{Name: "harnessly/notify", Permission: "notify", SDK: "hl.notify(text)", Doc: "A notification to the person."},
	{Name: "harnessly/files.read", Permission: "files.read", SDK: "hl.files.read(path)", Doc: "Read a workspace file inside the app's mask."},
	{Name: "harnessly/files.write", Permission: "files.write", SDK: "hl.files.write(path, text)", Doc: "Write a workspace file inside the app's mask."},
	{Name: "harnessly/tools.register", Permission: "tools.page", SDK: "hl.tools.register(tool, run)", Doc: "Offer the view's tools to the agent; the whole list each time."},
	{Name: "harnessly/tools.result", Permission: "tools.page", Doc: "The view's answer to an agent's call of its tool (the SDK sends it)."},
	{Name: "harnessly/context.publish", Permission: "context.publish", SDK: "hl.publish(data)", Doc: "What the view shows, for the agent to read with page_tools."},
	{Name: "harnessly/ui.toast", SDK: "hl.ui.toast(text)", Doc: "A short message at the bottom of the page."},
	{Name: "harnessly/ui.confirm", SDK: "hl.ui.confirm(text)", Doc: "A yes/no question in the host's own dialog."},
	{Name: "harnessly/slot.done", SDK: "hl.slot.done()", Doc: "A slot view (an onboarding step) says it is finished."},
	{Name: "harnessly/sinopia.document", Permission: "sinopia.read", SDK: "hl.sinopia.document()", Doc: "The document the view is shown in."},
	{Name: "harnessly/sinopia.selection", Permission: "sinopia.read", SDK: "hl.sinopia.selection()", Doc: "What the person selected."},
	{Name: "harnessly/sinopia.node", Permission: "sinopia.read", SDK: "hl.sinopia.node(id)", Doc: "One node of the document."},
	{Name: "harnessly/sinopia.query", Permission: "sinopia.read", SDK: "hl.sinopia.query(selector, options)", Doc: "Nodes matching a selector."},
	{Name: "harnessly/sinopia.fields", Permission: "sinopia.read", SDK: "hl.sinopia.fields()", Doc: "The values of the bound fields."},
	{Name: "harnessly/sinopia.mark", Permission: "sinopia.read", SDK: "hl.sinopia.mark(nodes, options)", Doc: "Highlight nodes in the editor."},
	{Name: "harnessly/sinopia.patch", Permission: "sinopia.write", SDK: "hl.sinopia.patch(ops)", Doc: "Change the document."},
	{Name: "harnessly/sinopia.setFields", Permission: "sinopia.write", SDK: "hl.sinopia.setFields(patch)", Doc: "Write bound fields back."},
	{Name: "harnessly/sinopia.boards.add", Permission: "sinopia.write", SDK: "hl.sinopia.boards.add(options)", Doc: "Add a board to the document."},
	{Name: "harnessly/sinopia.render", Permission: "sinopia.export", SDK: "hl.sinopia.render(options)", Doc: "Render the document."},
	{Name: "harnessly/sinopia.comment", Permission: "sinopia.comment", SDK: "hl.sinopia.comment(node, text)", Doc: "Comment on a node of the document."},
}

var surfaceTools = []SurfaceTool{
	{Name: "open", Doc: "Open an app's view in the person's right pane."},
	{Name: "data_get", Doc: "Read what the app keeps for this chat under a key."},
	{Name: "data_set", Doc: "Write what the app keeps for this chat under a key."},
	{Name: "settings_get", Doc: "Read the app's settings."},
	{Name: "ask", Doc: "Put a choice to the person in the app's view and wait for the answer."},
	{Name: "widget_show", Doc: "Show an app's chat view in the feed with the data given."},
	{Name: "widget_render", Doc: "Draw a widget written on the spot in the feed."},
	{Name: "page_tools", Doc: "The tools and the context the open views of the chat's apps offer from the page."},
	{Name: "page_call", Doc: "Call a tool an open view offers; the view runs it in the person's page."},
	{Name: "catalog", Doc: "The places, rights, methods and contribution kinds an app can use."},
	{Name: "sinopia_*", Doc: "Read and change the Sinopia document beside the chat."},
}

// The tables the check and the bridge read, made from the surface once.
var (
	methodPermission       = map[string]string{}
	permissions            = map[string]bool{}
	unsupportedPermissions = map[string]bool{}
	paramPermissions       = map[string]bool{}
)

func init() {
	for _, m := range surfaceMethods {
		methodPermission[m.Name] = m.Permission
	}
	for _, p := range surfacePermissions {
		switch {
		case p.Param != "":
			paramPermissions[p.Name] = true
		default:
			permissions[p.Name] = true
		}
		if p.Unsupported {
			unsupportedPermissions[p.Name] = true
		}
	}
}

// Catalog is the surface as `harnessly app catalog` and the agent's catalog tool give it.
type Catalog struct {
	Targets       []SurfaceTarget     `json:"targets"`
	Permissions   []SurfacePermission `json:"permissions"`
	Methods       []SurfaceMethod     `json:"methods"`
	Contributions []ContributionKind  `json:"contributions"`
	Tools         []SurfaceTool       `json:"tools"`
}

// SurfaceCatalog is the whole surface.
func SurfaceCatalog() Catalog {
	return Catalog{
		Targets: surfaceTargets, Permissions: surfacePermissions, Methods: surfaceMethods,
		Contributions: contributionKinds(), Tools: surfaceTools,
	}
}

// PermissionNames are the rights in the order the surface lists them.
func PermissionNames() []string {
	out := make([]string, 0, len(surfacePermissions))
	for _, p := range surfacePermissions {
		out = append(out, p.Name)
	}
	return out
}

// methodsNeeding are the methods a right opens, sorted.
func methodsNeeding(permission string) []string {
	var out []string
	for _, m := range surfaceMethods {
		if m.Permission == permission {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}
