package agent

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// MachineFile is machine.json: who this machine is to a hub and the two private keys it proved it with. It is written
// once, by `harnessly-agent register`, readable by its account only.
type MachineFile struct {
	MachineID       string           `json:"machineId"`
	HubURL          string           `json:"hubUrl"`
	DialURL         string           `json:"dialUrl"`
	RuntimeBasePath string           `json:"runtimeBasePath"`
	Profile         hubproto.Profile `json:"profile"`
	// Ed25519 is the signing key's 32-byte seed, X25519 the sealing key's private key; both base64.
	Ed25519      string `json:"ed25519"`
	X25519       string `json:"x25519"`
	RegisteredAt string `json:"registeredAt"`
}

// DefaultMachineFile is where machine.json lives in a data folder.
func DefaultMachineFile(dataDir string) string { return filepath.Join(dataDir, "machine.json") }

// LoadMachine reads and checks machine.json. os.ErrNotExist is returned (wrapped) when there is none.
func LoadMachine(path string) (*MachineFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m MachineFile
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

func (m *MachineFile) validate() error {
	if m.MachineID == "" || m.DialURL == "" || m.HubURL == "" {
		return errors.New("machineId, hubUrl and dialUrl are required")
	}
	if !strings.HasPrefix(m.RuntimeBasePath, "/") || strings.HasSuffix(m.RuntimeBasePath, "/") {
		return errors.New("runtimeBasePath must start with a slash and not end with one")
	}
	if !m.Profile.Valid() {
		return fmt.Errorf("unknown profile %q", m.Profile)
	}
	if seed, err := base64.StdEncoding.DecodeString(m.Ed25519); err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("ed25519 is not a 32-byte seed")
	}
	if priv, err := base64.StdEncoding.DecodeString(m.X25519); err != nil || len(priv) != 32 {
		return errors.New("x25519 is not a 32-byte key")
	}
	return nil
}

// SigningKey is the Ed25519 key the dial is signed with.
func (m *MachineFile) SigningKey() ed25519.PrivateKey {
	seed, _ := base64.StdEncoding.DecodeString(m.Ed25519)
	return ed25519.NewKeyFromSeed(seed)
}

// SealingKey is the X25519 private key envelopes are opened with.
func (m *MachineFile) SealingKey() []byte {
	priv, _ := base64.StdEncoding.DecodeString(m.X25519)
	return priv
}

// Save writes the file: 0600 in a folder of 0700, replaced whole.
func (m *MachineFile) Save(path string) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".machine-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// machineKeys are a fresh pair of keys and what the hub is told of them.
type machineKeys struct {
	seed, enc         []byte
	publicKey, encKey string
}

func newMachineKeys() (machineKeys, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return machineKeys{}, err
	}
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return machineKeys{}, err
	}
	return machineKeys{
		seed: priv.Seed(), enc: x.Bytes(),
		publicKey: base64.StdEncoding.EncodeToString(pub),
		encKey:    base64.StdEncoding.EncodeToString(x.PublicKey().Bytes()),
	}, nil
}

func (k machineKeys) file(res hubproto.RegisterResponse, profile hubproto.Profile, now time.Time) *MachineFile {
	return &MachineFile{
		MachineID: res.MachineID, HubURL: res.HubURL, DialURL: res.DialURL, RuntimeBasePath: res.RuntimeBasePath, Profile: profile,
		Ed25519:      base64.StdEncoding.EncodeToString(k.seed),
		X25519:       base64.StdEncoding.EncodeToString(k.enc),
		RegisteredAt: now.UTC().Format(time.RFC3339),
	}
}
