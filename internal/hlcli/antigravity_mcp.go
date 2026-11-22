package hlcli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// antigravityMCP is a launch-bound stdio transport for the existing stateless host
// MCP endpoint. Configuration contains only this command, never an HTTP bearer token.
func (c *cli) antigravityMCP() error {
	if c.env.Getenv("HARNESSLY_ANTIGRAVITY_CONVERSATION") == "" || c.env.Getenv("HARNESSLY_ANTIGRAVITY_GENERATION") == "" || c.session() == "" {
		return fail("Antigravity MCP requires a Harnessly-owned native launch")
	}
	if c.env.Getenv("HARNESSLY_CLI_TOKEN_FILE") == "" {
		return fail("Antigravity MCP requires a launch-scoped CLI token file")
	}
	getenv := c.env.Getenv
	c.env.Getenv = func(key string) string {
		if key == "HARNESSLY_CLI_TOKEN" {
			return ""
		}
		return getenv(key)
	}
	if c.env.Client == nil {
		c.env.Client = &http.Client{Timeout: 6 * time.Minute}
	}
	in := c.env.Stdin
	if in == nil {
		in = &emptyReader{}
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	out := json.NewEncoder(c.env.Stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(line, &request) != nil {
			return errors.New("Antigravity MCP received malformed JSON-RPC")
		}
		raw, err := c.request(http.MethodPost, "/api/cli/preview/mcp", nil, json.RawMessage(line))
		if len(request.ID) == 0 {
			continue
		}
		if err != nil {
			if err := out.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "Harnessly MCP request failed"}}); err != nil {
				return err
			}
			continue
		}
		if !json.Valid(raw) {
			return errors.New("Harnessly MCP returned invalid JSON")
		}
		if _, err := c.env.Stdout.Write(append(bytes.TrimSpace(raw), '\n')); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }
