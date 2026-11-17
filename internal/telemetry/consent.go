package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Built in at release build time (-ldflags -X ...telemetry.DefaultURL=... -X ...telemetry.DefaultKey=...): where a
// released runtime sends its anonymous reports unless told otherwise. A checkout carries neither and sends nothing.
var DefaultURL, DefaultKey string

// offWords are the values that turn telemetry off.
var offWords = map[string]bool{"off": true, "0": true, "false": true, "no": true, "none": true, "disable": true, "disabled": true}

// EnvOff says whether the environment turns telemetry off, and by which variable: RUNTIME_TELEMETRY or
// HARNESSLY_TELEMETRY set to off (also 0, false, no, none, disabled), or DO_NOT_TRACK set to anything but 0 or false
// (https://consoledonottrack.com).
func EnvOff(getenv func(string) string) (bool, string) {
	for _, k := range []string{"RUNTIME_TELEMETRY", "HARNESSLY_TELEMETRY"} {
		if offWords[strings.ToLower(strings.TrimSpace(getenv(k)))] {
			return true, k
		}
	}
	if v := strings.ToLower(strings.TrimSpace(getenv("DO_NOT_TRACK"))); v != "" && v != "0" && v != "false" {
		return true, "DO_NOT_TRACK"
	}
	return false, ""
}

// Consent is the machine's choice about telemetry, kept in a file of the data directory, with the random id its
// reports carry in place of a person (never an e-mail, a host name or a path). The environment's off wins over the
// file: an operator turns it off for every machine they set up, a person turns it off in Settings.
type Consent struct {
	Path   string
	Getenv func(string) string

	mu sync.Mutex
}

type consentFile struct {
	// Off is the person's choice; absent means on.
	Off bool   `json:"off,omitempty"`
	ID  string `json:"id"`
}

func (c *Consent) getenv(k string) string {
	if c.Getenv != nil {
		return c.Getenv(k)
	}
	return os.Getenv(k)
}

func (c *Consent) read() consentFile {
	var f consentFile
	if raw, err := os.ReadFile(c.Path); err == nil {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

func (c *Consent) write(f consentFile) error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	raw, _ := json.Marshal(f)
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

// Allowed says whether reports may leave the machine now; it is read at every push and every send, so a change takes
// effect at once.
func (c *Consent) Allowed() bool {
	if c == nil {
		return false
	}
	if off, _ := EnvOff(c.getenv); off {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.read().Off
}

// Why says what decides the state, for the settings and the start line: the variable that turns it off, "settings",
// or "" when it is on.
func (c *Consent) Why() string {
	if off, k := EnvOff(c.getenv); off {
		return k
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.read().Off {
		return "settings"
	}
	return ""
}

// Set keeps the person's choice. Turning it off also forgets the id: turned on again, the machine is a new one.
func (c *Consent) Set(on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.read()
	f.Off = !on
	if !on {
		f.ID = ""
	}
	return c.write(f)
}

// ID is the machine's random id, made on first use.
func (c *Consent) ID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	f := c.read()
	if f.ID == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		f.ID = hex.EncodeToString(b)
		_ = c.write(f)
	}
	return f.ID
}
