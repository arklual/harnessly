package sshinstall_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeServer is an SSH server that accepts one public key and answers every command through a handler.
type fakeServer struct {
	t       *testing.T
	ln      net.Listener
	host    ssh.Signer
	allowed ssh.PublicKey
	handler func(cmd string) (string, int)

	mu    sync.Mutex
	cmds  []string
	auths int
}

// startServer listens on a loopback port. authorized is an authorized_keys line; nothing else may sign in.
func startServer(t *testing.T, authorized string, handler func(cmd string) (string, int)) *fakeServer {
	t.Helper()
	allowed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorized))
	if err != nil {
		t.Fatalf("authorized key: %v", err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	host, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeServer{t: t, ln: ln, host: host, allowed: allowed, handler: handler}
	t.Cleanup(func() { ln.Close() })
	go f.serve()
	return f
}

func (f *fakeServer) addr() string { return f.ln.Addr().String() }

func (f *fakeServer) fingerprint() string { return ssh.FingerprintSHA256(f.host.PublicKey()) }

// authAttempts is how many times a client offered a key.
func (f *fakeServer) authAttempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auths
}

// commands are the commands received so far.
func (f *fakeServer) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cmds...)
}

func (f *fakeServer) serve() {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			f.mu.Lock()
			f.auths++
			f.mu.Unlock()
			if string(key.Marshal()) == string(f.allowed.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	cfg.AddHostKey(f.host)
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.conn(c, cfg)
	}
}

func (f *fakeServer) conn(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go f.session(ch, creqs)
	}
}

func (f *fakeServer) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" || len(req.Payload) < 4 {
			_ = req.Reply(false, nil)
			continue
		}
		n := binary.BigEndian.Uint32(req.Payload)
		cmd := unwrapSh(string(req.Payload[4 : 4+n]))
		_ = req.Reply(true, nil)
		f.mu.Lock()
		f.cmds = append(f.cmds, cmd)
		f.mu.Unlock()
		out, status := f.handler(cmd)
		_, _ = ch.Write([]byte(out))
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
		return
	}
}

// unwrapSh does what the server's shell does with `sh -c '<script>'`: it hands over the script.
func unwrapSh(cmd string) string {
	inner, ok := strings.CutPrefix(cmd, "sh -c '")
	if !ok || !strings.HasSuffix(inner, "'") {
		return cmd
	}
	return strings.ReplaceAll(strings.TrimSuffix(inner, "'"), `'\''`, `'`)
}
