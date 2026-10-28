package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// RegisterOptions are what `harnessly-agent register` was given.
type RegisterOptions struct {
	Hub     string // the hub's public URL
	Code    string // the one-time registration code, hx_…
	Profile hubproto.Profile
	Via     hubproto.Via // "" means command
	// MachineFile is where machine.json goes.
	MachineFile string
	// Force registers again over an existing machine.json.
	Force bool
	// Name is what the machine calls itself to the hub ("" means the host's name).
	Name    string
	Version string
	// HTTP is the client to call the hub with (nil: one with a 30 s timeout).
	HTTP *http.Client
	Now  func() time.Time
}

const registerTimeout = 30 * time.Second

// Reusable returns the registration in machineFile when it is valid, belongs to hub and has the profile: running the
// installer again on a registered machine then updates the programs and leaves the one machine the hub already has.
// It asks nothing of the hub, so a code that was used already does not matter.
func Reusable(machineFile, hub string, profile hubproto.Profile) (*MachineFile, bool) {
	m, err := LoadMachine(machineFile)
	if err != nil || m.Profile != profile {
		return nil, false
	}
	same := func(a, b string) bool {
		return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), "/"), strings.TrimRight(strings.TrimSpace(b), "/"))
	}
	return m, same(m.HubURL, hub)
}

// hubError is the hub's error body.
type hubError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Register exchanges the code for a machine id: it makes the machine's keys, tells the hub their public halves, and
// writes machine.json with the private ones. The file is written only after the hub said yes, and never over another
// registration without Force. The error's text is what the person should read.
func Register(ctx context.Context, o RegisterOptions) (*MachineFile, error) {
	hub := strings.TrimRight(strings.TrimSpace(o.Hub), "/")
	u, err := url.Parse(hub)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("--hub must be the hub's address, like https://hub.example.com (got %q)", o.Hub)
	}
	code := strings.TrimSpace(o.Code)
	if code == "" {
		return nil, errors.New("the registration code is empty")
	}
	if !o.Profile.Valid() {
		return nil, fmt.Errorf("--profile must be dir, container or full (got %q)", o.Profile)
	}
	via := o.Via
	if via == "" {
		via = hubproto.ViaCommand
	}
	if !via.Valid() {
		return nil, fmt.Errorf("--via must be command, ssh, local or container (got %q)", via)
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("%s machines are not supported, only Linux and macOS", runtime.GOOS)
	}
	if o.MachineFile == "" {
		return nil, errors.New("no place for machine.json")
	}
	if existing, err := LoadMachine(o.MachineFile); err == nil && !o.Force {
		return nil, fmt.Errorf("this machine is already registered as %s with %s; use --force to register it again", existing.MachineID, existing.HubURL)
	}

	keys, err := newMachineKeys()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	if name := strings.TrimSpace(o.Name); name != "" {
		if len(name) > 253 || strings.ContainsFunc(name, unicode.IsControl) {
			return nil, errors.New("--name is too long or has control characters")
		}
		host = name
	}
	unixUser := ""
	if cu, err := user.Current(); err == nil {
		unixUser = cu.Username
	}
	body, err := json.Marshal(hubproto.RegisterRequest{
		Code: code, PublicKey: keys.publicKey, EncKey: keys.encKey, Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH,
		UnixUser: unixUser, Profile: o.Profile, Via: via, AgentVersion: o.Version,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hub+"/agent/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := o.HTTP
	if client == nil {
		client = &http.Client{Timeout: registerTimeout}
	}
	res, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("could not reach the hub at %s: %w", hub, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		var he hubError
		_ = json.Unmarshal(raw, &he)
		msg := he.Message
		if msg == "" {
			msg = he.Error
		}
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
			if len(msg) > 200 {
				msg = msg[:200]
			}
		}
		return nil, fmt.Errorf("the hub did not register this machine (%d): %s", res.StatusCode, msg)
	}
	var out hubproto.RegisterResponse
	if err := json.Unmarshal(raw, &out); err != nil || out.MachineID == "" || out.DialURL == "" {
		return nil, errors.New("the hub's answer to the registration was not understood")
	}
	if out.HubURL == "" {
		out.HubURL = hub
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	m := keys.file(out, o.Profile, now())
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("the hub's answer is not usable: %w", err)
	}
	if err := m.Save(o.MachineFile); err != nil {
		return nil, fmt.Errorf("registered as %s, but machine.json was not written: %w", out.MachineID, err)
	}
	return m, nil
}
