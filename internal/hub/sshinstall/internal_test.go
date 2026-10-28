package sshinstall

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":      "'plain'",
		"it's":       `'it'\''s'`,
		"$(rm -rf)":  `'$(rm -rf)'`,
		"a b":        "'a b'",
		"":           "''",
		"line\nfeed": "'line\nfeed'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

// The quoting is not only a string match: sh must read back what went in.
func TestInShRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	hostile := `it's "a" $(echo pwned) ` + "`echo pwned`" + ` ; echo pwned`
	out, err := exec.Command("sh", "-c", inSh("printf %s "+shellQuote(hostile))).Output()
	if err != nil || string(out) != hostile {
		t.Errorf("round trip = %q, %v", out, err)
	}
}

func TestParseFacts(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want facts
	}{
		{"linux", "os=Linux\narch=x86_64\nuid=0\ncurl=1\nsudo=1\nfree_kb=1024\nmem_kb=2048\n",
			facts{OS: "linux", Arch: "amd64", Curl: true, Sudo: true, FreeBytes: 1 << 20, MemTotalBytes: 2 << 20}},
		{"mac", "os=Darwin\narch=arm64\nuid=501\ncurl=1\nsudo=0\nfree_kb=10\nmem_bytes=17179869184\n",
			facts{OS: "darwin", Arch: "arm64", UID: 501, Curl: true, FreeBytes: 10 << 10, MemTotalBytes: 16 << 30}},
		{"aarch64", "os=Linux\narch=aarch64\n", facts{OS: "linux", Arch: "arm64"}},
		{"noise around", "Welcome!\r\nos=Linux\r\narch=amd64\r\nmotd=x=y\n", facts{OS: "linux", Arch: "amd64"}},
		{"nothing", "", facts{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseFacts(c.in); got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// The look-around script is shell: run it here and check that it prints what parseFacts reads.
func TestPreflightScriptRuns(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	out, err := exec.Command("sh", "-c", preflightScript(`"$HOME"`)).Output()
	if err != nil {
		t.Fatal(err)
	}
	f := parseFacts(string(out))
	if f.OS == "" || f.Arch == "" || f.FreeBytes == 0 || f.MemTotalBytes == 0 {
		t.Errorf("script printed %q, parsed %+v", out, f)
	}
}

func TestInstallScript(t *testing.T) {
	cases := []struct {
		profile string
		uid     int
		sudo    bool
	}{
		{"dir", 0, false}, {"dir", 1000, true}, {"container", 1000, true}, {"full", 1000, false}, {"full", 0, false},
	}
	for _, c := range cases {
		got := installScript("https://hub.example.com", "hx_abc", c.profile, c.uid)
		if strings.Contains(got, "sudo -n") != c.sudo {
			t.Errorf("%s uid %d: sudo = %v in %s", c.profile, c.uid, !c.sudo, got)
		}
		for _, want := range []string{"curl -fsSL 'https://hub.example.com/install' -o \"$f\" || exit 1", "--token 'hx_abc'", "--profile '" + c.profile + "'"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in %s", c.profile, want, got)
			}
		}
	}
}

// A failed download must fail the install: the script is fetched to a file, not piped into sh.
func TestInstallScriptFailsWhenTheDownloadFails(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte("#!/bin/sh\nexit 22\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", installScript("https://hub.example.com", "hx_abc", "full", 1000))
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	if err := cmd.Run(); err == nil {
		t.Error("the install succeeded without a download")
	}
}

func TestProblem(t *testing.T) {
	ok := facts{OS: "linux", Arch: "amd64", Curl: true, Sudo: true, FreeBytes: 10 * gib, MemTotalBytes: 4 * gib}
	if p := ok.problem("dir"); p != "" {
		t.Errorf("a healthy server: %q", p)
	}
	// Unknown free space or memory does not block: the machine's own preflight checks again after the install.
	unknown := ok
	unknown.FreeBytes, unknown.MemTotalBytes = 0, 0
	if p := unknown.problem("dir"); p != "" {
		t.Errorf("unknown sizes: %q", p)
	}
	edge := ok
	edge.FreeBytes = minFree
	if p := edge.problem("dir"); p != "" {
		t.Errorf("exactly the minimum: %q", p)
	}
	edge.FreeBytes = minFree - 1
	if p := edge.problem("dir"); p == "" {
		t.Error("just under the minimum passed")
	}
}

func TestSummary(t *testing.T) {
	cases := []struct {
		f    facts
		want string
	}{
		{facts{FreeBytes: 412 * gib, MemTotalBytes: 64 * gib}, "412 ГБ свободно · 64 ГБ памяти"},
		{facts{MemTotalBytes: 16*gib - 100<<20}, "16 ГБ памяти"},
		{facts{}, ""},
	}
	for _, c := range cases {
		if got := c.f.summary(); got != c.want {
			t.Errorf("summary = %q, want %q", got, c.want)
		}
	}
}

func TestTailAndRedact(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line " + string(rune('a'+i))
	}
	got := tail(strings.Join(lines, "\n") + "\n")
	if n := strings.Count(got, "\n") + 1; n != tailLines || !strings.HasSuffix(got, "line t") {
		t.Errorf("tail = %q", got)
	}
	if got := redact("token hx_secret here, hx_secret again", "hx_secret"); strings.Contains(got, "hx_secret") {
		t.Errorf("redact = %q", got)
	}
	if got := redact("nothing", ""); got != "nothing" {
		t.Errorf("redact with no code = %q", got)
	}
}

func TestPublicAddr(t *testing.T) {
	cases := map[string]bool{
		// the open internet
		"8.8.8.8": true, "93.184.216.34": true, "1.1.1.1": true, "2606:4700:4700::1111": true, "2a00:1450:4001::1": true,
		// IPv4: loopback, private, link-local (the cloud metadata address), carrier-grade NAT, the special-purpose ranges
		"127.0.0.1": false, "127.255.255.254": false, "10.1.2.3": false, "172.16.0.1": false, "172.31.255.255": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "100.127.255.255": false, "0.0.0.0": false, "0.1.2.3": false, "224.0.0.1": false,
		"239.255.255.255": false, "240.0.0.1": false, "255.255.255.255": false, "192.0.0.8": false, "192.0.2.1": false, "192.88.99.1": false,
		"198.18.0.1": false, "198.19.255.255": false, "198.51.100.7": false, "203.0.113.7": false,
		// IPv6: not global unicast
		"::": false, "::1": false, "fe80::1": false, "fc00::1": false, "fd12:3456::1": false, "ff02::1": false, "fec0::1": false,
		"2001:db8::1": false, "3fff::1": false, "2001::1": false, "2001:2::1": false, "2001:10::1": false, "100::1": false,
		// IPv4 spelled inside IPv6: only as public as the address inside
		"::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false, "::ffff:169.254.169.254": false, "::ffff:8.8.8.8": true, "::ffff:7f00:1": false,
		"64:ff9b::7f00:1": false, "64:ff9b::a00:1": false, "64:ff9b::a9fe:a9fe": false, "64:ff9b::808:808": true, "64:ff9b:1::1": false,
		"2002:7f00:1::1": false, "2002:a00:1::": false, "2002:c0a8:101::1": false, "2002:808:808::1": true,
		// Teredo: the server's address and the client's (stored inverted) both count
		"2001:0:808:808::f7f7:f7f7":            true,
		"2001:0:4136:e378:8000:63bf:3fff:fdd2": false, // client 192.0.2.45
		"2001:0:7f00:1::f7f7:f7f7":             false, // server 127.0.0.1
		"2001:0:808:808::80ff:fffe":            false, // client 127.0.0.1 (stored inverted: 80ff:fffe)
		"::8.8.8.8":                            false, // IPv4-compatible, deprecated
		"::7f00:1":                             false,
		"fe80::1%eth0":                         false,
		"2606:4700:4700::1111%eth0":            false,
	}
	for in, want := range cases {
		a, err := netip.ParseAddr(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := publicAddr(a); got != want {
			t.Errorf("publicAddr(%s) = %v, want %v", in, got, want)
		}
	}
	if publicAddr(netip.Addr{}) {
		t.Error("a missing address is public")
	}
}

func TestNetworkDialerBlocksPrivateAddressesUnlessAllowed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	conn, err := networkDialer(true)(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("a dial with private addresses allowed: %v", err)
	}
	conn.Close()

	for _, addr := range []string{ln.Addr().String(), "localhost:22", "[::1]:22", "[::ffff:127.0.0.1]:22", "10.0.0.1:22", "169.254.169.254:80"} {
		if _, err := networkDialer(false)(context.Background(), "tcp", addr); !errors.Is(err, errBlocked) {
			t.Errorf("dial of %s: %v, want errBlocked", addr, err)
		}
	}
}

// A name is resolved once, all of what it answers has to be public, and the connection is made to an address that was
// checked, not to the name again: a name that answers one thing to the check and another to the connection (DNS
// rebinding) has no second answer to give.
func TestNetworkDialerResolvesOnceAndDialsTheCheckedAddress(t *testing.T) {
	defer func(old func(context.Context, string) ([]netip.Addr, error)) { lookupNetIP = old }(lookupNetIP)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan struct{}, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- struct{}{}
			c.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	var calls int
	answers := [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}, {netip.MustParseAddr("8.8.8.8")}}
	lookupNetIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		calls++
		return answers[min(calls-1, len(answers)-1)], nil
	}
	// allowed: the one lookup is what is dialed (the second answer is never asked for)
	conn, err := networkDialer(true)(context.Background(), "tcp", "rebind.example.com:"+port)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if calls != 1 {
		t.Errorf("%d lookups for one dial", calls)
	}
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Error("the connection did not go to the address that was looked up")
	}

	// not allowed: one private address among the answers refuses the lot, whatever order they come in
	calls = 0
	answers = [][]netip.Addr{{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.5")}}
	if _, err := networkDialer(false)(context.Background(), "tcp", "mixed.example.com:22"); !errors.Is(err, errBlocked) {
		t.Errorf("mixed answer: %v", err)
	}
	answers = [][]netip.Addr{{netip.MustParseAddr("10.0.0.5"), netip.MustParseAddr("8.8.8.8")}}
	calls = 0
	if _, err := networkDialer(false)(context.Background(), "tcp", "mixed.example.com:22"); !errors.Is(err, errBlocked) {
		t.Errorf("mixed answer, private first: %v", err)
	}
	// a name that does not resolve is an error, not a pass
	lookupNetIP = func(context.Context, string) ([]netip.Addr, error) { return nil, nil }
	if _, err := networkDialer(false)(context.Background(), "tcp", "nothing.example.com:22"); err == nil || errors.Is(err, errBlocked) {
		t.Errorf("an empty answer: %v", err)
	}
}

func TestMasterKey(t *testing.T) {
	dir := t.TempDir()
	k1, err := masterKey(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, SecretKeyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("key file: %v %v", info, err)
	}
	k2, err := masterKey(dir, "")
	if err != nil || string(k1) != string(k2) || len(k1) != 32 {
		t.Errorf("the key changed between starts: %v", err)
	}
	// a configured key wins and leaves the folder alone
	other := t.TempDir()
	c1, _ := masterKey(other, "0123456789abcdef0123456789abcdef")
	c2, _ := masterKey(other, "0123456789abcdef0123456789abcdeg")
	if string(c1) == string(k1) || string(c1) == string(c2) || len(c1) != 32 {
		t.Error("configured keys are not distinct, or not 32 bytes")
	}
	if _, err := os.Stat(filepath.Join(other, SecretKeyFile)); err == nil {
		t.Error("a key file was made next to a configured key")
	}
}

func TestBrokenSecretKeyFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SecretKeyFile), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := masterKey(dir, ""); err == nil {
		t.Error("a broken key file was accepted (and the keys it protects would be lost)")
	}
}

func TestSpaceKeysSealAndOpen(t *testing.T) {
	master, _ := masterKey(t.TempDir(), "")
	signer, pub, enc, err := spaceKey(master, "space-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 AAAA") || !strings.HasSuffix(pub, " harnessly") || strings.ContainsAny(pub, "\r\n") {
		t.Errorf("public line = %q", pub)
	}
	back, err := openSpaceKey(master, "space-1", enc)
	if err != nil || string(back.PublicKey().Marshal()) != string(signer.PublicKey().Marshal()) {
		t.Fatalf("opened key: %v", err)
	}
	if _, err := openSpaceKey(master, "space-2", enc); err == nil {
		t.Error("a key of one space opened for another")
	}
	other, _ := masterKey(t.TempDir(), "")
	if _, err := openSpaceKey(other, "space-1", enc); err == nil {
		t.Error("a key opened with another master key")
	}
	if _, err := openSpaceKey(master, "space-1", enc[:len(enc)-8]); err == nil {
		t.Error("a truncated key opened")
	}
	_, _, enc2, _ := spaceKey(master, "space-1")
	if enc == enc2 {
		t.Error("two keys are the same")
	}
}

func TestLimitedKeepsTheStart(t *testing.T) {
	l := &limited{max: 5}
	_, _ = l.Write([]byte("abc"))
	n, _ := l.Write([]byte("defgh"))
	if n != 5 || l.String() != "abcde" {
		t.Errorf("limited = %q (wrote %d)", l.String(), n)
	}
}
