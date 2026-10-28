package delivery

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

// ToolName is the MCP tool that hands a file over, the same act as `harnessly deliver`.
const ToolName = "deliver_file"

// Tools is the delivery's MCP tool for one calling agent. Cwd is the folder of the agent's chat ("" when the runtime
// could not tell), which a relative path is taken from.
type Tools struct {
	Access *files.Access
	Cwd    string
	// Handed is told of a file that was handed over (the runtime opens it in the chat's pane); nil for nothing.
	Handed func(Info)
}

var _ mcpserver.Toolset = Tools{}

func (Tools) Instructions() string {
	return "Handing files over: deliver_file gives the person a file you made for them (a zip, a docx, a pdf, a document) as a card " +
		"in the chat that opens it on the right and downloads it. Deliver only a file the person asked to receive; the files you " +
		"merely write or change make no card."
}

func (Tools) Tools() []any {
	return []any{jsvalue.NewObject(
		"name", ToolName,
		"description", "Hand a file to the person: it appears in the chat as a card they can open in the right pane (a zip shows its "+
			"contents) and download. Call it only for a file the person asked to receive; files you just edit or write as part of the work "+
			"are not delivered. The file must lie inside the workspace (or /tmp). Call it again after changing the file to hand over the new version.",
		"inputSchema", jsvalue.NewObject(
			"type", "object",
			"properties", jsvalue.NewObject(
				"path", jsvalue.NewObject("type", "string", "description", "The file: an absolute path, or relative to this chat's folder."),
				"title", jsvalue.NewObject("type", "string", "description", "What the card is called; omit for the file's name."),
			),
			"required", []any{"path"},
		),
	)}
}

func (Tools) Has(name string) bool { return name == ToolName }

func (t Tools) Call(_ context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	path, _ := args.Get("path")
	title, _ := args.Get("title")
	p, _ := path.(string)
	ttl, _ := title.(string)
	if strings.TrimSpace(p) == "" {
		return mcpserver.TextResult("refused: path is required", true)
	}
	if !filepath.IsAbs(p) {
		if t.Cwd == "" {
			return mcpserver.TextResult("refused: give an absolute path (the machine could not tell this chat's folder)", true)
		}
		p = filepath.Join(t.Cwd, p)
	}
	info, refusal := Describe(t.Access, filepath.Clean(p), ttl)
	if refusal != nil {
		return mcpserver.TextResult("refused: "+refusal.Text, true)
	}
	if t.Handed != nil {
		t.Handed(info)
	}
	return mcpserver.TextResult(Message(info), false)
}
