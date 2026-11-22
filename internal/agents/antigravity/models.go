package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
)

const MaxModelsOutput = 256 << 10
const MaxModels = 256

type Model struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ParseModels accepts a JSON list (or {models:[...]}) and the CLI's plain
// `id  label` listing. It never invents IDs from display labels.
func ParseModels(data []byte) ([]Model, error) {
	if len(data) > MaxModelsOutput {
		return nil, errors.New("Antigravity models output too large")
	}
	data = bytes.TrimSpace(data)
	var out []Model
	seen := map[string]bool{}
	add := func(id, label string) error {
		if !agents.ModelIDRe.MatchString(id) {
			return errors.New("Invalid Antigravity model ID")
		}
		if seen[id] {
			return nil
		}
		if len(out) >= MaxModels || len(label) > 512 {
			return errors.New("Antigravity model listing exceeds limits")
		}
		if label == "" {
			label = id
		}
		out = append(out, Model{id, label})
		seen[id] = true
		return nil
	}
	if len(data) > 0 && (data[0] == '[' || data[0] == '{') {
		var list []json.RawMessage
		if data[0] == '{' {
			var envelope struct {
				Models []json.RawMessage `json:"models"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				return nil, err
			}
			list = envelope.Models
		} else if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		for _, raw := range list {
			var id string
			if json.Unmarshal(raw, &id) == nil {
				if err := add(id, id); err != nil {
					return nil, err
				}
				continue
			}
			var m struct {
				ID    string `json:"id"`
				Label string `json:"label"`
				Name  string `json:"name"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
			if m.Label == "" {
				m.Label = m.Name
			}
			if err := add(m.ID, m.Label); err != nil {
				return nil, err
			}
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || line == "Available models:" || strings.EqualFold(line, "ID  Name") {
				continue
			}
			words := strings.Fields(line)
			if len(words) == 0 {
				continue
			}
			id := words[0]
			label := strings.TrimSpace(strings.TrimPrefix(line, id))
			if err := add(id, label); err != nil {
				return nil, err
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("Antigravity returned no models")
	}
	return out, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxModelsOutput-b.Len() {
		return 0, errors.New("Antigravity models output too large")
	}
	return b.Buffer.Write(p)
}

// DiscoverModels runs a bounded, cancellable native CLI query. No cache or login
// inference belongs here. Callers decide when network-backed discovery is allowed.
func DiscoverModels(ctx context.Context, binary string) ([]Model, error) {
	if binary == "" {
		binary = Binary
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "models")
	var output boundedBuffer
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return ParseModels(output.Bytes())
}
