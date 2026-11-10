package filesync

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"
)

// ProfileCodec is a person's Claude profile (filestore.Profile), the codec design-sync's profile
// rooms were written with: machine paths become placeholders, the settings that describe the machine stay on it, and
// the contents are sealed with the room key before they leave the machine. The settings' environment variables that
// carry a secret (a token, a key, a password) stay on the machine too: secrets go through the hub's safe only.
type ProfileCodec struct {
	Home      string
	Workspace string
	// LocalKeys are settings.json keys (dotted for nested ones, "env.MY_ROOT") that stay on the machine.
	LocalKeys []string
	key       []byte
}

// DefaultLocalKeys: hooks and the status line call this machine's integrations, the API helpers are this machine's
// logins. A deployment adds the env variables that hold its own paths (RUNTIME_PROFILE_LOCAL_KEYS).
var DefaultLocalKeys = []string{"hooks", "statusLine", "apiKeyHelper", "awsAuthRefresh", "awsCredentialExport",
	"env.CCGUI_CLI_LOGIN_AUTHORIZED"}

// secretEnv is the name of an environment variable that carries a secret by its look.
var secretEnv = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|ACCESS_?KEY|PRIVATE_?KEY|CREDENTIAL|AUTH|COOKIE|SESSION|_KEY$|^KEY$|_PAT$|DSN$)`)

// secretValue is a value that is a token by its look, whatever its variable is called.
var secretValue = regexp.MustCompile(`^(glpat-|gldt-|ghp_|gho_|ghs_|github_pat_|sk-|xox[abprs]-|AKIA|ya29\.|eyJ[A-Za-z0-9_-]{10,}\.)`)

// SecretEnv tells whether a settings env variable stays on the machine as a secret, by its name or its value.
func SecretEnv(name string, value any) bool {
	if secretEnv.MatchString(name) {
		return true
	}
	v, ok := value.(string)
	return ok && secretValue.MatchString(strings.TrimSpace(v))
}

// RoomKeySize is the size of a profile room's key.
const RoomKeySize = chacha20poly1305.KeySize

// SetKey installs the room key (32 bytes).
func (p *ProfileCodec) SetKey(k []byte) { p.key = append([]byte(nil), k...) }

// Mergeable: settings.json merges key by key.
func (p *ProfileCodec) Mergeable(rel string) bool { return rel == profileSettings }

func (p *ProfileCodec) Ready() bool { return len(p.key) == RoomKeySize }

func isText(b []byte) bool { return utf8.Valid(b) && !bytes.ContainsRune(b, 0) }

// Placeholders stand for this machine's paths in canonical contents.
const (
	homeMark      = "@@HOME@@"
	workspaceMark = "@@WORKSPACE@@"
)

const profileSettings = ".claude/settings.json"

func (p *ProfileCodec) toMarks(b []byte) []byte {
	if !isText(b) {
		return b
	}
	s := string(b)
	// The longer path first: the workspace usually lives inside HOME.
	pairs := [][2]string{{p.Workspace, workspaceMark}, {p.Home, homeMark}}
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i][0]) > len(pairs[j][0]) })
	for _, pr := range pairs {
		if pr[0] != "" && pr[0] != "/" {
			s = strings.ReplaceAll(s, pr[0], pr[1])
		}
	}
	return []byte(s)
}

func (p *ProfileCodec) fromMarks(b []byte) []byte {
	if !isText(b) {
		return b
	}
	s := string(b)
	if p.Workspace != "" {
		s = strings.ReplaceAll(s, workspaceMark, p.Workspace)
	}
	s = strings.ReplaceAll(s, homeMark, p.Home)
	return []byte(s)
}

// localKeys are the settings keys of doc that stay on the machine: the configured ones and every secret env variable.
func (p *ProfileCodec) localKeys(docs ...map[string]any) [][]string {
	var out [][]string
	for _, k := range p.LocalKeys {
		out = append(out, strings.Split(k, "."))
	}
	seen := map[string]bool{}
	for _, doc := range docs {
		env, _ := doc["env"].(map[string]any)
		for name, v := range env {
			if SecretEnv(name, v) && !seen[name] {
				seen[name] = true
				out = append(out, []string{"env", name})
			}
		}
	}
	return out
}

func (p *ProfileCodec) Canonical(rel string, local []byte) []byte {
	b := p.toMarks(local)
	if rel != profileSettings {
		return b
	}
	var doc map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return b
	}
	for _, k := range p.localKeys(doc) {
		deletePath(doc, k)
	}
	return marshalSettings(doc)
}

func (p *ProfileCodec) Local(rel string, canonical, current []byte) []byte {
	b := p.fromMarks(canonical)
	if rel != profileSettings {
		return b
	}
	var doc, cur map[string]any
	if json.Unmarshal(b, &doc) != nil {
		return b
	}
	if current != nil && json.Unmarshal(current, &cur) == nil {
		// A secret that came down from an older room is not taken: the machine's own value (or none) stays.
		for _, k := range p.localKeys(doc, cur) {
			deletePath(doc, k)
			if v, ok := getPath(cur, k); ok {
				setPath(doc, k, v)
			}
		}
	} else {
		for _, k := range p.localKeys(doc) {
			deletePath(doc, k)
		}
	}
	return marshalSettings(doc)
}

// Tag is a keyed fingerprint: the hub cannot test a guess of a file's contents against it.
func (p *ProfileCodec) Tag(c []byte) string {
	m := hmac.New(sha256.New, p.key)
	m.Write(c)
	return hex.EncodeToString(m.Sum(nil))
}

func (p *ProfileCodec) Seal(rel string, c []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(p.key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(c)+aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// The path is bound in: a blob cannot be replayed as another file.
	return aead.Seal(nonce, nonce, c, []byte(rel)), nil
}

var errSealed = errors.New("blob does not open with the room key")

func (p *ProfileCodec) Open(rel string, b []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(p.key)
	if err != nil {
		return nil, err
	}
	if len(b) < aead.NonceSize() {
		return nil, errSealed
	}
	out, err := aead.Open(nil, b[:aead.NonceSize()], b[aead.NonceSize():], []byte(rel))
	if err != nil {
		return nil, errSealed
	}
	return out, nil
}

// Merge merges settings.json key by key (recursively for objects): a key changed on one side only takes that side,
// a key changed on both takes the newer edit. Two machines editing different settings never conflict.
func (p *ProfileCodec) Merge(rel string, base, local, remote []byte, localNewer bool) ([]byte, bool) {
	if rel != profileSettings {
		return nil, false
	}
	var b, l, r map[string]any
	if json.Unmarshal(base, &b) != nil || json.Unmarshal(local, &l) != nil || json.Unmarshal(remote, &r) != nil {
		return nil, false
	}
	return marshalSettings(merge3(b, l, r, localNewer)), true
}

func merge3(base, local, remote map[string]any, localNewer bool) map[string]any {
	out := map[string]any{}
	keys := map[string]bool{}
	for _, m := range []map[string]any{base, local, remote} {
		for k := range m {
			keys[k] = true
		}
	}
	for k := range keys {
		bv, bok := base[k]
		lv, lok := local[k]
		rv, rok := remote[k]
		lChanged := lok != bok || !reflect.DeepEqual(lv, bv)
		rChanged := rok != bok || !reflect.DeepEqual(rv, bv)
		bm, bIsMap := bv.(map[string]any)
		lm, lIsMap := lv.(map[string]any)
		rm, rIsMap := rv.(map[string]any)
		switch {
		case lChanged && rChanged && lIsMap && rIsMap:
			if !bIsMap {
				bm = map[string]any{}
			}
			out[k] = merge3(bm, lm, rm, localNewer)
		case lChanged && rChanged:
			if localNewer {
				if lok {
					out[k] = lv
				}
			} else if rok {
				out[k] = rv
			}
		case lChanged:
			if lok {
				out[k] = lv
			}
		default:
			if rok {
				out[k] = rv
			}
		}
	}
	return out
}

func marshalSettings(doc map[string]any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(doc)
	return buf.Bytes()
}

func getPath(m map[string]any, keys []string) (any, bool) {
	v, ok := m[keys[0]]
	if !ok || len(keys) == 1 {
		return v, ok
	}
	sub, isMap := v.(map[string]any)
	if !isMap {
		return nil, false
	}
	return getPath(sub, keys[1:])
}

func setPath(m map[string]any, keys []string, v any) {
	if len(keys) == 1 {
		m[keys[0]] = v
		return
	}
	sub, ok := m[keys[0]].(map[string]any)
	if !ok {
		sub = map[string]any{}
		m[keys[0]] = sub
	}
	setPath(sub, keys[1:], v)
}

func deletePath(m map[string]any, keys []string) {
	if len(keys) == 1 {
		delete(m, keys[0])
		return
	}
	if sub, ok := m[keys[0]].(map[string]any); ok {
		deletePath(sub, keys[1:])
		if len(sub) == 0 {
			delete(m, keys[0])
		}
	}
}
