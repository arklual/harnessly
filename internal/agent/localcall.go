package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ErrNoAgent: no agent.json lies in the data directory, so no machine agent runs for this account.
var ErrNoAgent = errors.New("no machine agent on this machine")

// CallLocal posts a JSON body to a loopback route of the machine agent whose agent.json lies in dataDir, and returns
// the status and body of its answer. It is how the runtime reaches the cluster for documents.
func CallLocal(client *http.Client, dataDir, tokenHeader, route string, body any, timeout time.Duration) (int, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "agent.json"))
	if err != nil {
		return 0, nil, ErrNoAgent
	}
	var info struct {
		Port  any    `json:"port"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &info) != nil || info.Token == "" {
		return 0, nil, ErrNoAgent
	}
	port := ""
	switch p := info.Port.(type) {
	case float64:
		if p != 0 {
			port = strconv.FormatFloat(p, 'f', -1, 64)
		}
	case string:
		port = p
	}
	if port == "" {
		return 0, nil, ErrNoAgent
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+route, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("content-type", "application/json")
	if tokenHeader == "" {
		tokenHeader = "x-agent-local-token"
	}
	req.Header.Set(tokenHeader, info.Token)
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return res.StatusCode, data, err
}
