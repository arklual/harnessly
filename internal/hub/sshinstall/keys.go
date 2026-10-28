package sshinstall

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SecretKeyFile is the file in the data folder the hub's encryption key is made in when HUB_SECRET_KEY is not set.
const SecretKeyFile = "secret_key"

// masterKey is the key that encrypts the private halves of the spaces' SSH keys. It is HUB_SECRET_KEY when that is set
// (a Secret of the cluster, a file next to the database's credentials), else 32 random bytes in the data folder, made
// on the first start (0600; O_EXCL, so that a second hub process starting at the same moment reads what the other
// wrote). Either way it is not in the database: a copy of the database alone does not open the keys.
func masterKey(dataDir, configured string) ([]byte, error) {
	if configured != "" {
		sum := sha256.Sum256([]byte("harnessly-hub/secret-key/v1\x00" + configured))
		return sum[:], nil
	}
	path := filepath.Join(dataDir, SecretKeyFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw, err = makeSecretFile(path)
	}
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("%s: not a key the hub made (%d bytes)", path, len(raw))
	}
	return raw, nil
}

func makeSecretFile(path string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return key, nil
}

// spaceKey is a new Ed25519 key pair for a space: the signer, the public half as an authorized_keys line, and the
// private half encrypted for the database.
func spaceKey(master []byte, spaceID string) (signer ssh.Signer, pub, enc string, err error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "harnessly")
	if err != nil {
		return nil, "", "", err
	}
	if signer, err = ssh.NewSignerFromKey(priv); err != nil {
		return nil, "", "", err
	}
	enc, err = seal(master, spaceID, pem.EncodeToMemory(block))
	if err != nil {
		return nil, "", "", err
	}
	return signer, publicLine(signer.PublicKey()), enc, nil
}

// openSpaceKey decrypts a stored private half and returns its signer.
func openSpaceKey(master []byte, spaceID, enc string) (ssh.Signer, error) {
	raw, err := open(master, spaceID, enc)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(raw)
}

// The encryption is AES-256-GCM with a random nonce in front of the ciphertext. The space id is authenticated with it:
// a row copied to another space does not decrypt there.
func aad(spaceID string) []byte { return []byte("harnessly-hub/space-ssh-key/v1\x00" + spaceID) }

func gcm(master []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(master []byte, spaceID string, plain []byte) (string, error) {
	g, err := gcm(master)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, plain, aad(spaceID))), nil
}

func open(master []byte, spaceID, enc string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	g, err := gcm(master)
	if err != nil {
		return nil, err
	}
	if len(data) < g.NonceSize() {
		return nil, errors.New("the stored key is too short")
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], aad(spaceID))
}

// publicLine is the key as an authorized_keys line: "ssh-ed25519 AAAA… harnessly".
func publicLine(pub ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))) + " harnessly"
}
