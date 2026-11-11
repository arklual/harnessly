package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
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
//	RUNTIME_FLEET_SERVICES    name=health-address pairs, comma-separated: the services are restarted one at a time,
//	                          RUNTIME_FLEET_RESTART runs once for each with its name in FLEET_SERVICE
//	RUNTIME_FLEET_SETTLE_TIMEOUT  seconds the next service waits for the chats of the one before it (300)
//	RUNTIME_FLEET_MIN_GAP     the shortest time between two activations ("24h"); unset or 0 is no window
//	RUNTIME_FLEET_UNITS_SPEC  a units spec (fleet_units.go): Activate writes the machine's unit files from it and
//	                          reloads systemd once; unset leaves the units alone. RUNTIME_FLEET_UNIT_DIR is their
//	                          folder (/etc/systemd/system), RUNTIME_FLEET_HOST the machine's name in the spec
//	                          (the short hostname)
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
		f.Probe = func(ctx context.Context) (string, error) {
			h, err := probeHealth(ctx, url)
			if err == nil && h.Chats == ChatsStarting {
				return "", errors.New("its chats are not loaded")
			}
			return h.Version, err
		}
	}
	for _, pair := range strings.FieldsFunc(getenv("RUNTIME_FLEET_SERVICES"), func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		name, url, ok := strings.Cut(pair, "=")
		if !ok || name == "" || url == "" {
			continue
		}
		f.Services = append(f.Services, Service{Name: name, Probe: func(ctx context.Context) (Health, error) { return probeHealth(ctx, url) }})
	}
	if cmd := strings.TrimSpace(getenv("RUNTIME_FLEET_RESTART")); cmd != "" && len(f.Services) > 0 {
		f.RestartOne = func(ctx context.Context, name string) error {
			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			c.Env = append(os.Environ(), "FLEET_SERVICE="+name)
			out, err := c.CombinedOutput()
			if err != nil {
				return fmt.Errorf("restart: %w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(getenv("RUNTIME_FLEET_SETTLE_TIMEOUT"))); err == nil && n > 0 {
		f.SettleTimeout = time.Duration(n) * time.Second
	}
	f.MinGap = GapFromEnv(getenv("RUNTIME_FLEET_MIN_GAP"), 0)
	if path := strings.TrimSpace(getenv("RUNTIME_FLEET_UNITS_SPEC")); path != "" {
		spec, err := ReadUnitsSpec(path)
		if err != nil {
			// A spec that does not read stops the activation rather than letting it run with the old units.
			f.UnitsErr = err
		} else {
			f.Units = &spec
		}
		f.UnitDir = strings.TrimSpace(getenv("RUNTIME_FLEET_UNIT_DIR"))
		f.Host = strings.TrimSpace(getenv("RUNTIME_FLEET_HOST"))
		if f.Host == "" {
			f.Host = ShortHostname()
		}
	}
	return f
}

// GapFromEnv reads a window between updates: a duration ("24h", "90m"), or 0 or off for none; def when unset or
// unreadable.
func GapFromEnv(raw string, def time.Duration) time.Duration {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "":
		return def
	case "0", "off", "no", "false":
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return def
	}
	return d
}

// probeHealth reads what a service reports at url: its version, and how far its chats are when it says.
func probeHealth(ctx context.Context, url string) (Health, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Health{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Health{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
		Chats   string `json:"chats"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body); err != nil {
		return Health{}, err
	}
	return Health{Version: body.Version, Chats: body.Chats}, nil
}

// ShortHostname is this machine's name up to the first dot.
func ShortHostname() string {
	h, _ := os.Hostname()
	h, _, _ = strings.Cut(h, ".")
	return h
}
