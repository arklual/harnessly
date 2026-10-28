package agent

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The commands of `harnessly-agent` besides running: register, off, on, status, policy. They are short, read their
// folders from the same variables the agent does (AGENT_DATA_DIR, AGENT_MACHINE_FILE, AGENT_POLICY_FILE), and fall
// back to the agent.env the installer wrote, so `sudo harnessly-agent status` looks where the service does.

const cliUsage = `usage: harnessly-agent [command]

  (no command)   run the agent
  register       --hub URL (--token CODE | --code-file PATH) --profile dir|container|full [--via command|ssh|local|container] [--name NAME] [--force | --reuse]
  unlink         remove this machine from its hub (signed with the machine's own key); the files stay
  gitkey-remove  take away the git key and the ssh config block the agent made (what was there before stays)
  status         one line: linked to <hub> as <machine>, off, not registered…; exit 0 when linked
  off            switch the agent off on this machine: the hub link closes and stays closed
  on             switch it on again
  policy         print the local policy in force
  version
`

// systemEnvFiles are the places the installer leaves the agent's folders for these commands.
func systemEnvFiles(home string) []string {
	files := []string{"/etc/harnessly/agent.env"}
	if home != "" {
		files = append(files, filepath.Join(home, ".config", "harnessly", "agent.env"))
	}
	return files
}

// cliEnvKeys are the variables an agent.env may supply to the commands.
var cliEnvKeys = map[string]bool{"AGENT_DATA_DIR": true, "AGENT_MACHINE_FILE": true, "AGENT_POLICY_FILE": true}

// loadAgentEnv sets, from the first agent.env that exists, the folder variables the environment does not have.
func loadAgentEnv(files []string, lookup func(string) (string, bool), set func(k, v string)) {
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			k, v, ok := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			if !ok || !cliEnvKeys[k] {
				continue
			}
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			if _, have := lookup(k); !have && v != "" {
				set(k, v)
			}
		}
		return
	}
}

// cliPaths are the folders the commands work in.
type cliPaths struct {
	home        string
	dataDir     string
	machineFile string
	policy      *PolicyStore
}

func resolveCLIPaths() (cliPaths, error) {
	home := env("HOME", "")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return cliPaths{}, err
		}
		home = h
	}
	loadAgentEnv(systemEnvFiles(home), func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok && v != "" }, func(k, v string) { _ = os.Setenv(k, v) })
	dataDir := env("AGENT_DATA_DIR", filepath.Join(home, ".harnessly"))
	return cliPaths{
		home: home, dataDir: dataDir,
		machineFile: env("AGENT_MACHINE_FILE", DefaultMachineFile(dataDir)),
		policy:      NewPolicyStore(env("AGENT_POLICY_FILE", ""), home, dataDir),
	}, nil
}

// RunCLI runs one of the commands and returns the process's exit code. handled is false for no command (run the agent).
func RunCLI(args []string, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) == 0 {
		return 0, false
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "register", "unlink", "gitkey-remove", "status", "off", "on", "policy":
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, BuildVersion)
		return 0, true
	case "help", "--help", "-h":
		fmt.Fprint(stdout, cliUsage)
		return 0, true
	default:
		fmt.Fprintf(stderr, "harnessly-agent: unknown command %q\n\n%s", cmd, cliUsage)
		return 2, true
	}
	paths, err := resolveCLIPaths()
	if err != nil {
		fmt.Fprintln(stderr, "harnessly-agent:", err)
		return 1, true
	}
	switch cmd {
	case "register":
		return cmdRegister(rest, paths, stdout, stderr), true
	case "unlink":
		return cmdUnlink(paths, stdout, stderr), true
	case "gitkey-remove":
		return cmdGitKeyRemove(paths, stdout, stderr), true
	case "status":
		return cmdStatus(paths, time.Now(), stdout), true
	case "off":
		return cmdOff(paths, stdout, stderr), true
	case "on":
		return cmdOn(paths, stdout, stderr), true
	default:
		return cmdPolicy(paths, stdout), true
	}
}

func cmdRegister(args []string, paths cliPaths, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	hub := fs.String("hub", "", "the hub's address, https://hub.example.com")
	token := fs.String("token", "", "the one-time registration code from the hub (hx_…)")
	codeFile := fs.String("code-file", "", "read the code from this file")
	profile := fs.String("profile", "", "dir, container or full")
	via := fs.String("via", "command", "command, ssh, local or container")
	name := fs.String("name", "", "the name this machine shows in the hub (default: the host's name)")
	force := fs.Bool("force", false, "register again over an existing registration")
	reuse := fs.Bool("reuse", false, "keep a valid registration with this hub as it is, and register only when there is none")
	machineFile := fs.String("machine-file", paths.machineFile, "where machine.json goes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "harnessly-agent register: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *reuse && *force {
		fmt.Fprintln(stderr, "harnessly-agent register: give --reuse or --force, not both")
		return 2
	}
	if *reuse {
		if m, ok := Reusable(*machineFile, *hub, hubproto.Profile(*profile)); ok {
			fmt.Fprintf(stdout, "already registered as %s with %s (%s): kept, nothing sent to the hub\n", m.MachineID, m.HubURL, m.Profile)
			return 0
		}
		*force = true // another hub or profile, or no file: a new registration replaces what is there
	}
	code := strings.TrimSpace(*token)
	if *codeFile != "" {
		if code != "" {
			fmt.Fprintln(stderr, "harnessly-agent register: give --token or --code-file, not both")
			return 2
		}
		raw, err := os.ReadFile(*codeFile)
		if err != nil {
			fmt.Fprintf(stderr, "harnessly-agent register: the code file: %v\n", reason(err))
			return 1
		}
		code = strings.TrimSpace(string(raw))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	m, err := Register(ctx, RegisterOptions{
		Hub: *hub, Code: code, Profile: hubproto.Profile(*profile), Via: hubproto.Via(*via),
		MachineFile: *machineFile, Force: *force, Name: *name, Version: BuildVersion,
	})
	if err != nil {
		fmt.Fprintln(stderr, "harnessly-agent register:", err)
		return 1
	}
	fmt.Fprintf(stdout, "registered as %s with %s (%s); keys are in %s\n", m.MachineID, m.HubURL, m.Profile, *machineFile)
	return 0
}

// reason is an error without the path it already printed.
func reason(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// switchTargets are the places `off` may put the kill-switch file, best first: next to the system policy when this
// account can write there (the root of a `dir` machine), next to the account's, then the agent's data folder.
func (p cliPaths) switchTargets() []string {
	files := p.policy.SwitchFiles()
	var out []string
	for i, f := range files {
		dir := filepath.Dir(f)
		last := i == len(files)-1
		if _, err := os.Stat(dir); err == nil || last {
			out = append(out, f)
		}
	}
	return out
}

func cmdGitKeyRemove(paths cliPaths, stdout, stderr io.Writer) int {
	gone, err := RemoveGitKey(paths.home, paths.dataDir)
	for _, g := range gone {
		fmt.Fprintln(stdout, "removed", g)
	}
	if err != nil {
		fmt.Fprintln(stderr, "harnessly-agent gitkey-remove:", reason(err))
		return 1
	}
	if len(gone) == 0 {
		fmt.Fprintln(stdout, "no git key of the agent here")
	}
	return 0
}

func cmdOff(paths cliPaths, stdout, stderr io.Writer) int {
	for _, f := range paths.policy.SwitchFiles() {
		if _, err := os.Stat(f); err == nil {
			fmt.Fprintf(stdout, "already off (%s)\n", f)
			return 0
		}
	}
	var last error
	for _, f := range paths.switchTargets() {
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			last = err
			continue
		}
		body := "switched off " + time.Now().UTC().Format(time.RFC3339) + " by harnessly-agent off\n"
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			last = err
			continue
		}
		fmt.Fprintf(stdout, "off: %s\nThe hub link closes within a second and stays closed until `harnessly-agent on`.\n", f)
		return 0
	}
	fmt.Fprintf(stderr, "harnessly-agent off: could not write the switch file: %v\nStopping the service does the same: systemctl stop harnessly-agent\n", last)
	return 1
}

func cmdOn(paths cliPaths, stdout, stderr io.Writer) int {
	removed, failed := 0, 0
	for _, f := range paths.policy.SwitchFiles() {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if err := os.Remove(f); err != nil {
			fmt.Fprintf(stderr, "harnessly-agent on: could not remove %s: %v\n", f, reason(err))
			failed++
			continue
		}
		removed++
	}
	switch {
	case failed > 0:
		return 1
	case removed == 0:
		fmt.Fprintln(stdout, "already on")
	default:
		fmt.Fprintln(stdout, "on: the agent dials the hub again within a few seconds")
	}
	return 0
}

// cmdStatus prints one line and exits 0 only when the machine is linked to its hub.
func cmdStatus(paths cliPaths, now time.Time, stdout io.Writer) int {
	line, linked := statusLine(paths, now)
	fmt.Fprintln(stdout, line)
	if linked {
		return 0
	}
	return 1
}

func statusLine(paths cliPaths, now time.Time) (string, bool) {
	m, err := LoadMachine(paths.machineFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "not registered", false
	case err != nil:
		return "machine.json is damaged: " + err.Error(), false
	}
	for _, f := range paths.policy.SwitchFiles() {
		if _, err := os.Stat(f); err == nil {
			return "off (" + f + ")", false
		}
	}
	st, err := ReadLinkState(LinkStatePath(paths.dataDir))
	if err != nil || !st.Fresh(now) || st.MachineID != m.MachineID {
		return fmt.Sprintf("registered as %s with %s, but the agent is not running", m.MachineID, m.HubURL), false
	}
	if st.Linked {
		return fmt.Sprintf("linked to %s as %s", m.HubURL, m.MachineID), true
	}
	note := st.Note
	if note == "" {
		note = "no link yet"
	}
	return fmt.Sprintf("not linked to %s as %s: %s", m.HubURL, m.MachineID, note), false
}

func cmdPolicy(paths cliPaths, stdout io.Writer) int {
	paths.policy.Reload(true)
	raw, _ := json.MarshalIndent(paths.policy.Current(), "", "  ")
	fmt.Fprintln(stdout, string(raw))
	if paths.policy.Disabled() {
		fmt.Fprintln(stdout, "(the kill switch is on: the agent is off)")
	}
	if paths.policy.Soft() {
		fmt.Fprintln(stdout, "(this account can change the policy file, so while the agent runs a change of it that allows more waits for SIGHUP or a restart; "+
			"a root-owned file in /etc/harnessly applies at once and cannot be rewritten through the runtime)")
	}
	return 0
}
