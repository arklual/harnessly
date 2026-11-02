package apps

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// The mock MCP server of an eval: a stdio server that answers a package's tool calls from a fixture, so a scenario runs
// the agent against the app's tools without the app's real backend. `harnessly app mock-mcp <fixture>` runs it; the
// eval writes it into the .mcp.json of the copy of the package the agent runs on.
//
// The fixture is a file of the package:
//
//	{ "tools": [ { "name": "get_categories", "description": "…", "inputSchema": {…}, "result": ["fuel", "food"] } ] }
//
// `result` is what the tool returns: a string as it is, any other JSON as its text. A call to a tool the fixture does not
// have is an error answer the agent sees.

// MockTool is one tool of a fixture.
type MockTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Result      json.RawMessage `json:"result"`
}

// MockFixture is a fixture file.
type MockFixture struct {
	Tools []MockTool `json:"tools"`
}

// ReadMockFixture reads and checks a fixture.
func ReadMockFixture(path string) (MockFixture, error) {
	var f MockFixture
	raw, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, fmt.Errorf("%s: %v", path, err)
	}
	seen := map[string]bool{}
	for _, t := range f.Tools {
		if t.Name == "" || seen[t.Name] {
			return f, fmt.Errorf("%s: a tool has a name, once", path)
		}
		seen[t.Name] = true
	}
	return f, nil
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ServeMockMCP answers MCP over stdio (one JSON message per line) until the input ends.
func ServeMockMCP(in io.Reader, out io.Writer, f MockFixture) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	enc := json.NewEncoder(out)
	reply := func(id json.RawMessage, result any, rpcErr map[string]any) error {
		msg := map[string]any{"jsonrpc": "2.0", "id": id}
		if rpcErr != nil {
			msg["error"] = rpcErr
		} else {
			msg["result"] = result
		}
		return enc.Encode(msg)
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if json.Unmarshal([]byte(line), &req) != nil || req.Method == "" {
			continue
		}
		// A notification has no id and no answer.
		if len(req.ID) == 0 {
			continue
		}
		var err error
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2025-06-18"
			}
			err = reply(req.ID, map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "harnessly-eval-mock", "version": "1"},
			}, nil)
		case "ping":
			err = reply(req.ID, map[string]any{}, nil)
		case "tools/list":
			tools := make([]map[string]any, 0, len(f.Tools))
			for _, t := range f.Tools {
				schema := t.InputSchema
				if len(schema) == 0 {
					schema = json.RawMessage(`{"type":"object"}`)
				}
				tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": schema})
			}
			err = reply(req.ID, map[string]any{"tools": tools}, nil)
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			err = reply(req.ID, f.call(p.Name), nil)
		default:
			err = reply(req.ID, nil, map[string]any{"code": -32601, "message": "no method " + req.Method})
		}
		if err != nil {
			return err
		}
	}
	return sc.Err()
}

func (f MockFixture) call(name string) map[string]any {
	for _, t := range f.Tools {
		if t.Name != name {
			continue
		}
		text := string(t.Result)
		var s string
		if json.Unmarshal(t.Result, &s) == nil {
			text = s
		}
		if len(t.Result) == 0 {
			text = "ok"
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	}
	return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "the mock has no tool " + name}}}
}
