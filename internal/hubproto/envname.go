package hubproto

import (
	"fmt"
	"regexp"
	"strings"
)

// EnvNameRe is the shape of a variable name the hub sets on a machine: capital letters, digits and _, up to 64.
var EnvNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)

// A name `env.deliver` accepts looks like a secret: it ends in one of envSecretSuffixes. Everything else a hub could
// set (a variable that changes how git, a pager, a TLS stack, a package manager or an interpreter behaves) is refused,
// because the env files are sourced into every agent launch and a hub that can set GIT_SSH_COMMAND or PYTHONPATH can run
// code on the machine without a shell tab, past the policy's terminal switch. An address is not a secret: `_URL` and
// `_HOST` are not accepted, since the variables that point a package manager, a registry client or a CLI at another
// server (PIP_INDEX_URL, GH_HOST, a registry mirror) make the machine fetch and run what that server serves.
var envSecretSuffixes = []string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD", "_PAT", "_DSN", "_USER"}

// Names refused even when they end like a secret: variables that start code in a shell or a program, or that point a
// program at another trust store, another proxy or another configuration.
var envForbiddenNames = map[string]bool{
	"PATH": true, "HOME": true, "SHELL": true, "USER": true, "LOGNAME": true, "PWD": true, "IFS": true, "TMPDIR": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	"BASH_ENV": true, "ENV": true, "PROMPT_COMMAND": true, "PS1": true, "PS4": true, "ZDOTDIR": true,
	"NODE_OPTIONS": true, "NODE_PATH": true, "NODE_EXTRA_CA_CERTS": true, "JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true,
	"GCONV_PATH": true, "BROWSER": true, "PAGER": true, "SSH_ASKPASS": true, "EDITOR": true, "VISUAL": true,
	// Registries and hosts the tools fetch code from, by name.
	"GOPROXY": true, "GOFLAGS": true, "HF_ENDPOINT": true, "GH_HOST": true,
}

var envForbiddenPrefixes = []string{
	"LD_", "DYLD_", "BASH_FUNC_", "GIT_", "LESS", "SSL_CERT_", "PYTHON", "PERL", "RUBY",
	// Package managers and container tools: their settings are registries, mirrors, hooks and credential helpers.
	"PIP_", "UV_", "DOCKER_", "NPM_CONFIG_", "CARGO_", "CONDA_",
	"ANTHROPIC_", "OPENAI_", "HARNESSLY_", "RUNTIME_", "AGENT_", "CODEX_", "CLAUDE_",
}

// EnvFault says which rule a refused name broke.
type EnvFault int

const (
	EnvMalformed EnvFault = iota + 1 // not capital letters, digits and _, or longer than 64
	EnvReserved                      // a name or prefix the hub may not set (code, proxies, trust stores, agent keys)
	EnvNotSecret                     // allowed in shape, but it does not end like a secret
)

// EnvNameError is a name `env.deliver` refuses. Err's text is English and goes to the agent's answer; the hub maps the
// fault to its own words.
type EnvNameError struct {
	Fault EnvFault
	// Detail is the offending prefix, name or the list of accepted endings, per Fault.
	Detail string
	text   string
}

func (e *EnvNameError) Error() string { return e.text }

// CheckEnvName is the one rule of docs/hub/api.md for a name `env.deliver` accepts. The hub applies it before it
// relays and the agent applies it again: a hub that is itself compromised must not get past the machine's check. It
// returns nil or an *EnvNameError.
func CheckEnvName(name string) error {
	if !EnvNameRe.MatchString(name) {
		return &EnvNameError{Fault: EnvMalformed, text: "a variable name is capital letters, digits and _, up to 64, not starting with a digit"}
	}
	if envForbiddenNames[name] {
		return &EnvNameError{Fault: EnvReserved, Detail: name, text: fmt.Sprintf("%s is not set from the hub", name)}
	}
	for _, p := range envForbiddenPrefixes {
		if strings.HasPrefix(name, p) {
			return &EnvNameError{Fault: EnvReserved, Detail: p, text: fmt.Sprintf("names starting with %s are not set from the hub", p)}
		}
	}
	if strings.HasSuffix(name, "_PROXY") || strings.Contains(name, "_CA_") {
		return &EnvNameError{Fault: EnvReserved, Detail: name, text: fmt.Sprintf("%s changes how connections are made and is not set from the hub", name)}
	}
	for _, suffix := range envSecretSuffixes {
		if strings.HasSuffix(name, suffix) {
			return nil
		}
	}
	list := strings.Join(envSecretSuffixes, ", ")
	return &EnvNameError{Fault: EnvNotSecret, Detail: list, text: fmt.Sprintf("%s does not look like a secret: its name must end in %s", name, list)}
}
