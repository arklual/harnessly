package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
)

// The first-run password. A runtime that is reachable by others and has no password yet would let whoever opens the page
// first choose it. So the setup is guarded by an initial password (INIT_PASSWORD when the deployment sets one), and when
// it sets none and the runtime listens beyond this machine the runtime makes one at start and prints it once. The initial
// password only opens the setup: the person who knows it then chooses the real password, which is what gets stored. It
// lives in memory, so a restart before the setup is done prints a new one, and none is made once a password exists.

// initPasswordOut is where the generated password is printed (the runtime's stdout, so `docker logs` and journalctl have it).
var initPasswordOut io.Writer = os.Stdout

// newInitPassword makes the generated password; a test swaps it.
var newInitPassword = generateInitPassword

const (
	// No 0/O, 1/I/L or U: the password is read off a log and typed.
	initPasswordAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ" // gitleaks:allow (an alphabet, not a secret)
	initPasswordGroups   = 5
	initPasswordGroupLen = 4
)

// generateInitPassword: 20 characters of a 30-letter alphabet in groups of four (about 98 bits), from the system's CSPRNG.
func generateInitPassword() string {
	n := initPasswordGroups * initPasswordGroupLen
	raw := make([]byte, 0, n)
	buf := make([]byte, 64)
	for len(raw) < n {
		if _, err := rand.Read(buf); err != nil {
			panic("runtime: no randomness for the initial password: " + err.Error())
		}
		for _, b := range buf {
			// Rejection sampling: 240 is the largest multiple of 30 below 256, so every character is equally likely.
			if b < 240 && len(raw) < n {
				raw = append(raw, initPasswordAlphabet[int(b)%len(initPasswordAlphabet)])
			}
		}
	}
	var sb strings.Builder
	for i, c := range raw {
		if i > 0 && i%initPasswordGroupLen == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

// listensOnLoopback says whether the runtime's listener (RUNTIME_GATE_LISTEN, host:port) is bound to this machine's loopback
// address only. A listener that cannot be read as a host and port, or has no host, is not.
func listensOnLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

// wantsGeneratedInitPassword: whether this start makes the initial password itself. RUNTIME_GENERATE_INIT_PASSWORD=1 or 0
// forces it either way. Otherwise it is made for a standalone runtime that listens beyond loopback (a container, a server)
// and has no legacy AUTH_PASSWORD, and not for one on 127.0.0.1, where the person at the keyboard is the one who opens the
// page first (`make run`, an SSH tunnel). A runtime on loopback behind a reverse proxy is public all the same: it should
// set INIT_PASSWORD or RUNTIME_GENERATE_INIT_PASSWORD=1.
func wantsGeneratedInitPassword(upstream string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_GENERATE_INIT_PASSWORD"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return upstream == "" && os.Getenv("AUTH_PASSWORD") == "" && !listensOnLoopback(os.Getenv("RUNTIME_GATE_LISTEN"))
}

// announceInitPassword prints the generated password where the operator reads the log. The line starts with
// "Initial password:" so a grep for it finds it, and it is plain text, not a JSON log line.
func announceInitPassword(pw string) {
	fmt.Fprintf(initPasswordOut, `
================================================================================
  harnessly: no password is set yet, so the page is locked behind a one-time one.

  Initial password: %s

  Open the page and enter it; the next step asks you to choose your own password,
  and this one stops working. Until then it is valid only for this run: a restart
  prints a new one. To pick it yourself, start with INIT_PASSWORD set (4 characters
  or more); to turn this off, RUNTIME_GENERATE_INIT_PASSWORD=0.
================================================================================

`, pw)
}
