package apps

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/egress"
)

// The signature of an app package. A package is a .hlapp: the archive of Pack. Its signature is a small JSON file
// beside it, <name>.hlapp.sig: the publisher's name, the public key and an ed25519 signature of the archive's SHA-256.
// The key is the publisher's own (made by `harnessly app key`, kept in the machine's safe); a deployment lists the keys
// it trusts in its network policy (egress.Publisher) and may require a signature of every package it installs.

// SigSuffix is what is added to the name of a .hlapp for the name of its signature.
const SigSuffix = ".sig"

// sigContext is what the signature covers beside the archive's sum, so a signature of something else is not taken.
const sigContext = "harnessly-hlapp-v1\n"

// Signature is the content of <name>.hlapp.sig.
type Signature struct {
	Alg       string `json:"alg"`
	Publisher string `json:"publisher"`
	// Key is the public key, base64 of 32 bytes.
	Key string `json:"key"`
	// SHA256 is the archive's sum (hex); the signature is over it.
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

// Why a package was refused, in words for the person; errors.Is tells the kind.
var (
	ErrUnsigned     = errors.New("пакет не подписан")
	ErrUntrustedKey = errors.New("подпись не от доверенного издателя")
	ErrBadSignature = errors.New("подпись не сходится с пакетом")
)

// GenerateKey makes a publisher's key pair: the private key as base64 of its 64 bytes (to keep in the safe) and the
// public one as base64 of 32 bytes (to give to the deployment's admin).
func GenerateKey() (private, public string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub), nil
}

// PublicOf is the public key of a private one (base64 both).
func PublicOf(private string) (string, error) {
	priv, err := parsePrivate(private)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)), nil
}

func parsePrivate(s string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("ключ подписи не ed25519 (base64, 64 байта): создайте его командой harnessly app key")
	}
	return ed25519.PrivateKey(raw), nil
}

func signedBytes(sum string) []byte { return []byte(sigContext + sum) }

// Sign signs the archive as publisher with the private key (base64).
func Sign(archive []byte, publisher, private string) (Signature, error) {
	priv, err := parsePrivate(private)
	if err != nil {
		return Signature{}, err
	}
	publisher = strings.TrimSpace(publisher)
	if publisher == "" {
		return Signature{}, fmt.Errorf("у подписи нет издателя: укажите --publisher")
	}
	sum := ArchiveSum(archive)
	return Signature{
		Alg: "ed25519", Publisher: publisher, Key: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
		SHA256: sum, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signedBytes(sum))),
	}, nil
}

// Marshal is the signature as the file holds it.
func (s Signature) Marshal() []byte {
	raw, _ := json.MarshalIndent(s, "", "  ")
	return append(raw, '\n')
}

// ParseSignature reads a .sig file.
func ParseSignature(raw []byte) (*Signature, error) {
	var s Signature
	if err := json.Unmarshal(raw, &s); err != nil || s.Alg != "ed25519" || s.Key == "" || s.Signature == "" {
		return nil, fmt.Errorf("%w: файл подписи не читается", ErrBadSignature)
	}
	return &s, nil
}

// KeyID is a short fingerprint of a public key for messages (the first 8 bytes of its SHA-256, hex).
func KeyID(key string) string {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil {
		return "?"
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// Verified is what a successful check says: whose signature it is, when there was one.
type Verified struct {
	Publisher string
	// Signed: the archive carries a signature that matches it and a trusted key.
	Signed bool
}

// VerifyArchive checks an archive against its signature (nil: none) under the deployment's policy. A signature that does
// not match the archive is always refused. With RequireSigned a package without a signature, or signed by a key the
// policy does not list, is refused too; without it such a package is installed as unsigned.
func VerifyArchive(archive []byte, sig *Signature, policy *egress.Policy) (Verified, error) {
	require := policy != nil && policy.RequireSigned
	if sig == nil {
		if require {
			return Verified{}, fmt.Errorf("%w, а в этом контуре устанавливаются только подписанные приложения: попросите издателя подписать пакет (harnessly app pack --sign)", ErrUnsigned)
		}
		return Verified{}, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.Key))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return Verified{}, fmt.Errorf("%w: в подписи нет ключа ed25519", ErrBadSignature)
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sig.Signature))
	sum := ArchiveSum(archive)
	if err != nil || sig.SHA256 != sum || !ed25519.Verify(raw, signedBytes(sum), got) {
		return Verified{}, fmt.Errorf("%w: пакет изменили после подписи или подпись от другого пакета — возьмите файл у издателя заново", ErrBadSignature)
	}
	if policy != nil {
		for _, p := range policy.Publishers {
			if strings.TrimSpace(p.Key) == strings.TrimSpace(sig.Key) {
				return Verified{Publisher: p.Name, Signed: true}, nil
			}
		}
	}
	if require {
		return Verified{}, fmt.Errorf("%w: пакет подписан «%s» (ключ %s), а в контуре доверяют другим издателям — попросите администратора хаба добавить этот ключ",
			ErrUntrustedKey, sig.Publisher, KeyID(sig.Key))
	}
	return Verified{Publisher: sig.Publisher}, nil
}

// SignaturesRequired says whether this deployment installs signed packages only.
func SignaturesRequired() bool { return currentNetwork().RequireSigned }

// DevUnsignedWarning is what an install for development says where signatures are required: the folder is not checked
// (the author's own work), and that is a conscious exception, written in the app's journal.
const DevUnsignedWarning = "в этом контуре приложения ставятся только с подписью издателя, а установка для разработки (dev) подпись не проверяет: " +
	"это исключение для автора, оно записано в журнал приложения. Для обычной установки подпишите пакет: harnessly app pack --sign"

// refuseUnsigned is the refusal of an install that has no confirmed signature where signatures are required.
func refuseUnsigned(id string) error {
	return fmt.Errorf("%w: «%s» ставится без подтверждённой подписи, а в этом контуре устанавливаются только подписанные приложения — "+
		"возьмите у издателя .hlapp с подписью (harnessly app install файл.hlapp) или попросите администратора отключить требование", ErrUnsigned, id)
}
