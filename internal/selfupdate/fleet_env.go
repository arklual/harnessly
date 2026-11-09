package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// FleetOn says whether the environment asks for fleet mode: RUNTIME_UPDATE_MODE=fleet. Without it a runtime updates
// from its hub (or not at all) exactly as before.
func FleetOn(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv("RUNTIME_UPDATE_MODE")), "fleet")
}

// DefaultParts is what a delta replaces unless RUNTIME_FLEET_PARTS says otherwise.
var DefaultParts = []string{"bin", ReleaseFile}

// FleetFromEnv builds the machine's Fleet from its environment; nil when RUNTIME_FLEET_ROOT is not set.
//
//	RUNTIME_FLEET_ROOT        the folder holding releases/ and current
//	RUNTIME_FLEET_KEEP        releases kept (3)
//	RUNTIME_FLEET_PARTS       comma-separated top-level names of a release a delta may replace
//	RUNTIME_FLEET_RESTART     a shell command that restarts the services that run from <root>/current
//	RUNTIME_FLEET_HEALTH_URL  an address that answers 200 with {"version":"…"} once the services serve a release
//	RUNTIME_FLEET_HEALTH_TIMEOUT  seconds to wait for it (90)
func FleetFromEnv(getenv func(string) string, log func(string)) *Fleet {
	root := strings.TrimSpace(getenv("RUNTIME_FLEET_ROOT"))
	if root == "" {
		return nil
	}
	f := &Fleet{Root: root, Parts: DefaultParts, Log: log}
	if n, err := strconv.Atoi(strings.TrimSpace(getenv("RUNTIME_FLEET_KEEP"))); err == nil && n > 0 {
		f.Keep = n
	}
	if raw := strings.TrimSpace(getenv("RUNTIME_FLEET_PARTS")); raw != "" {
		f.Parts = nil
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				f.Parts = append(f.Parts, p)
			}
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(getenv("RUNTIME_FLEET_HEALTH_TIMEOUT"))); err == nil && n > 0 {
		f.HealthTimeout = time.Duration(n) * time.Second
	}
	if cmd := strings.TrimSpace(getenv("RUNTIME_FLEET_RESTART")); cmd != "" {
		f.Restart = func(ctx context.Context) error {
			out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
			if err != nil {
				return fmt.Errorf("restart: %w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	if url := strings.TrimSpace(getenv("RUNTIME_FLEET_HEALTH_URL")); url != "" {
		f.Probe = func(ctx context.Context) (string, error) { return probeVersion(ctx, url) }
	}
	return f
}

// probeVersion reads the version a service reports at url.
func probeVersion(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	return body.Version, nil
}
