package agent

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The machine's git key: one Ed25519 key made once at ~/.ssh/harnessly_ed25519, offered to the well-known git hosts by
// a marked block in ~/.ssh/config, so the person adds its public half to GitHub or GitLab and the agents can clone and
// push. It is made here, in the OpenSSH key format, so the machine needs no ssh-keygen. What was made, as opposed to
// what was already there, is written to gitkey.json in the data folder, and RemoveGitKey (the installer's --uninstall)
// takes away only that and the marked block.

const (
	gitKeyName   = "harnessly_ed25519"
	sshBlockFrom = "# >>> harnessly >>>"
	sshBlockTo   = "# <<< harnessly <<<"
	sshKeyType   = "ssh-ed25519"
	// gitHosts are the hosts the key is offered to: the git forges the person is most likely to use. The key is not
	// offered to every host, because every ssh server would get to see its public half. A forge of their own (a
	// self-hosted GitLab, say) is one more line in ~/.ssh/config: Host git.example.com, IdentityFile ~/.ssh/harnessly_ed25519.
	gitHosts      = "github.com gitlab.com bitbucket.org codeberg.org"
	gitKeyRecord  = "gitkey.json"
	sshConfigName = "config"
)

// gitKeyMade is what the agent made on this machine, as opposed to found there.
type gitKeyMade struct {
	Dir        bool `json:"dir,omitempty"`        // ~/.ssh
	Key        bool `json:"key,omitempty"`        // the private key
	Pub        bool `json:"pub,omitempty"`        // the .pub file
	ConfigFile bool `json:"configFile,omitempty"` // ~/.ssh/config
}

func readGitKeyMade(dataDir string) gitKeyMade {
	var made gitKeyMade
	if dataDir == "" {
		return made
	}
	if raw, err := os.ReadFile(filepath.Join(dataDir, gitKeyRecord)); err == nil {
		_ = json.Unmarshal(raw, &made)
	}
	return made
}

func (m gitKeyMade) save(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, gitKeyRecord), append(raw, '\n'), 0o600)
}

func sshString(b *bytes.Buffer, s []byte) {
	_ = binary.Write(b, binary.BigEndian, uint32(len(s)))
	b.Write(s)
}

func sshPublicBlob(pub ed25519.PublicKey) []byte {
	var b bytes.Buffer
	sshString(&b, []byte(sshKeyType))
	sshString(&b, pub)
	return b.Bytes()
}

// authorizedLine is the public key as GitHub and authorized_keys take it.
func authorizedLine(pub ed25519.PublicKey, comment string) string {
	return sshKeyType + " " + base64.StdEncoding.EncodeToString(sshPublicBlob(pub)) + " " + comment
}

// marshalOpenSSH writes an unencrypted Ed25519 private key in the format of `ssh-keygen`.
func marshalOpenSSH(priv ed25519.PrivateKey, comment string) []byte {
	pub := priv.Public().(ed25519.PublicKey)
	var check [4]byte
	_, _ = rand.Read(check[:])
	var inner bytes.Buffer
	inner.Write(check[:])
	inner.Write(check[:])
	sshString(&inner, []byte(sshKeyType))
	sshString(&inner, pub)
	sshString(&inner, priv) // seed followed by the public key, 64 bytes
	sshString(&inner, []byte(comment))
	for i := byte(1); inner.Len()%8 != 0; i++ {
		inner.WriteByte(i)
	}
	var out bytes.Buffer
	out.WriteString("openssh-key-v1\x00")
	sshString(&out, []byte("none"))
	sshString(&out, []byte("none"))
	sshString(&out, nil)
	_ = binary.Write(&out, binary.BigEndian, uint32(1))
	sshString(&out, sshPublicBlob(pub))
	sshString(&out, inner.Bytes())
	return pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: out.Bytes()})
}

type sshReader struct {
	b   []byte
	err error
}

func (r *sshReader) u32() uint32 {
	if len(r.b) < 4 {
		r.err = errors.New("short key")
		return 0
	}
	v := binary.BigEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

func (r *sshReader) str() []byte {
	n := r.u32()
	if r.err != nil || uint64(n) > uint64(len(r.b)) {
		r.err = errors.New("short key")
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

// publicOfOpenSSH reads the public key out of an unencrypted Ed25519 key file, for a key whose .pub went missing.
func publicOfOpenSSH(raw []byte) (ed25519.PublicKey, error) {
	blk, _ := pem.Decode(raw)
	if blk == nil || blk.Type != "OPENSSH PRIVATE KEY" {
		return nil, errors.New("not an OpenSSH private key")
	}
	const magic = "openssh-key-v1\x00"
	if !bytes.HasPrefix(blk.Bytes, []byte(magic)) {
		return nil, errors.New("not an OpenSSH private key")
	}
	r := &sshReader{b: blk.Bytes[len(magic):]}
	cipher, kdf := r.str(), r.str()
	_ = r.str()
	if r.err != nil || string(cipher) != "none" || string(kdf) != "none" {
		return nil, errors.New("the key is encrypted or not understood")
	}
	if r.u32() != 1 {
		return nil, errors.New("more than one key in the file")
	}
	blob := &sshReader{b: r.str()}
	if r.err != nil || string(blob.str()) != sshKeyType {
		return nil, errors.New("not an Ed25519 key")
	}
	pub := blob.str()
	if blob.err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("not an Ed25519 key")
	}
	return ed25519.PublicKey(pub), nil
}

// safeComment keeps a host name fit for a key comment.
func safeComment(host string) string {
	host = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, host)
	if host == "" {
		host = "machine"
	}
	return "harnessly@" + host
}

// EnsureGitKey is EnsureGitKeyIn without the record of what was made.
func EnsureGitKey(home, hostname string) (string, error) { return EnsureGitKeyIn(home, "", hostname) }

// EnsureGitKeyIn makes the git key when there is none and makes sure ssh offers it to the git hosts; it returns the
// public half. Run again, it changes nothing. What it makes is recorded in dataDir (when given) for RemoveGitKey.
func EnsureGitKeyIn(home, dataDir, hostname string) (string, error) {
	dir := filepath.Join(home, ".ssh")
	made := readGitKeyMade(dataDir)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		made.Dir = true
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	privPath := filepath.Join(dir, gitKeyName)
	pubPath := privPath + ".pub"
	var line string
	if raw, err := os.ReadFile(privPath); err == nil {
		pub, err := publicOfOpenSSH(raw)
		if err != nil {
			return "", fmt.Errorf("%s exists and is not a key harnessly can read: %w", privPath, err)
		}
		if old, err := os.ReadFile(pubPath); err == nil && strings.HasPrefix(string(old), sshKeyType+" ") {
			line = strings.TrimSpace(string(old))
		} else {
			line = authorizedLine(pub, safeComment(hostname))
			if err := writePublic(pubPath, line); err != nil {
				return "", err
			}
			made.Pub = true
		}
	} else if errors.Is(err, os.ErrNotExist) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", err
		}
		comment := safeComment(hostname)
		// O_EXCL: the key is ours only when this call made it.
		f, err := os.OpenFile(privPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		_, werr := f.Write(marshalOpenSSH(priv, comment))
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(privPath)
			return "", werr
		}
		made.Key = true
		line = authorizedLine(priv.Public().(ed25519.PublicKey), comment)
		if err := writePublic(pubPath, line); err != nil {
			_ = made.save(dataDir)
			return "", err
		}
		made.Pub = true
	} else {
		return "", err
	}
	cfgPath := filepath.Join(dir, sshConfigName)
	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		made.ConfigFile = true
	}
	if err := ensureSSHConfig(cfgPath); err != nil {
		_ = made.save(dataDir)
		return "", err
	}
	return line, made.save(dataDir)
}

// writePublic writes the .pub file the way ssh-keygen leaves it, readable by all whatever the umask.
func writePublic(path, line string) error {
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}

// sshBlock is the block that offers the key, between its two marker lines.
func sshBlock() string {
	return sshBlockFrom + "\nHost " + gitHosts + "\n  IdentityFile ~/.ssh/" + gitKeyName + "\n" + sshBlockTo + "\n"
}

// blockSpan finds the marked block in a config: where it starts and where it ends, the line break after the last
// marker included.
func blockSpan(config []byte) (from, to int, ok bool) {
	from = bytes.Index(config, []byte(sshBlockFrom))
	if from < 0 {
		return 0, 0, false
	}
	end := bytes.Index(config[from:], []byte(sshBlockTo))
	if end < 0 {
		return 0, 0, false
	}
	to = from + end + len(sshBlockTo)
	if to < len(config) && config[to] == '\n' {
		to++
	}
	return from, to, true
}

// ensureSSHConfig adds the marked block that offers the key to the git hosts, once; a block of an earlier version is
// brought up to date in place. The person's own configuration outside the markers stays as it was.
func ensureSSHConfig(path string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if from, to, ok := blockSpan(existing); ok {
		if string(existing[from:to]) == sshBlock() {
			return nil
		}
		return replaceFile(path, append(append(append([]byte{}, existing[:from]...), sshBlock()...), existing[to:]...))
	}
	// Appended, never rewritten: nothing of the person's file can be lost.
	var b bytes.Buffer
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteByte('\n')
	}
	if len(existing) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(sshBlock())
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(b.Bytes())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// replaceFile writes a file whole through a temporary file, keeping the mode of the one it replaces.
func replaceFile(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".harnessly-ssh-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// RemoveGitKey undoes EnsureGitKeyIn: the marked block leaves ~/.ssh/config, and what the agent made (the key, its
// .pub, the config file, the folder, as the record says) goes. A key, a file or a folder that was there before the
// agent came stays. It returns what it removed.
func RemoveGitKey(home, dataDir string) ([]string, error) {
	made := readGitKeyMade(dataDir)
	dir := filepath.Join(home, ".ssh")
	var gone []string
	rm := func(path string) error {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		gone = append(gone, path)
		return nil
	}
	cfgPath := filepath.Join(dir, sshConfigName)
	if existing, err := os.ReadFile(cfgPath); err == nil {
		if from, to, ok := blockSpan(existing); ok {
			// The blank line EnsureGitKeyIn put before the block goes with it.
			if from > 0 && existing[from-1] == '\n' && (from == 1 || existing[from-2] == '\n') {
				from--
			}
			rest := append(append([]byte{}, existing[:from]...), existing[to:]...)
			if made.ConfigFile && len(bytes.TrimSpace(rest)) == 0 {
				if err := rm(cfgPath); err != nil {
					return gone, err
				}
			} else {
				if err := replaceFile(cfgPath, rest); err != nil {
					return gone, err
				}
				gone = append(gone, cfgPath+" (the harnessly block)")
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return gone, err
	}
	privPath := filepath.Join(dir, gitKeyName)
	if made.Key {
		if err := rm(privPath); err != nil {
			return gone, err
		}
	}
	if made.Pub {
		if err := rm(privPath + ".pub"); err != nil {
			return gone, err
		}
	}
	if made.Dir {
		_ = os.Remove(dir) // only when empty
	}
	if dataDir != "" {
		_ = os.Remove(filepath.Join(dataDir, gitKeyRecord))
	}
	return gone, nil
}
