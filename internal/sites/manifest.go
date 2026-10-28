package sites

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Limits bound one deployment and one space.
type Limits struct {
	MaxDeployBytes   int64
	MaxFileBytes     int64
	MaxFiles         int
	MaxSitesPerSpace int
	KeepVersions     int
}

// DefaultLimits are the limits of v1.
var DefaultLimits = Limits{
	MaxDeployBytes:   100 << 20,
	MaxFileBytes:     32 << 20,
	MaxFiles:         5000,
	MaxSitesPerSpace: 50,
	KeepVersions:     20,
}

const maxPathLen = 512

// File is one file of a deployment: its content's sha256 (lower-case hex) and size.
type File struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

// Manifest is a deployment: path (slash separated, relative) → file.
type Manifest map[string]File

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidHash says whether s is a lower-case hex sha256.
func ValidHash(s string) bool { return hashRe.MatchString(s) }

// CleanPath turns a path of an upload into the manifest's form or refuses it: no absolute paths, no "..", no empty
// segments, no backslashes, no control characters, nothing under .git.
func CleanPath(p string) (string, error) {
	if p == "" || len(p) > maxPathLen {
		return "", errors.New("путь пустой или длиннее 512 символов")
	}
	if strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return "", fmt.Errorf("путь %q не подходит", p)
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("путь %q не подходит", p)
		}
	}
	p = strings.TrimPrefix(p, "./")
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." {
			return "", fmt.Errorf("путь %q не подходит", p)
		}
		if s == ".git" {
			return "", fmt.Errorf("путь %q: папку .git не публикуем", p)
		}
	}
	if path.Clean(p) != p {
		return "", fmt.Errorf("путь %q не подходит", p)
	}
	return p, nil
}

// Validate checks every path, hash and size and the limits.
func (m Manifest) Validate(l Limits) error {
	if len(m) == 0 {
		return errors.New("в сайте нет файлов")
	}
	if len(m) > l.MaxFiles {
		return fmt.Errorf("файлов больше %d", l.MaxFiles)
	}
	var total int64
	for p, f := range m {
		if c, err := CleanPath(p); err != nil || c != p {
			if err == nil {
				err = fmt.Errorf("путь %q не подходит", p)
			}
			return err
		}
		if !ValidHash(f.Hash) {
			return fmt.Errorf("%s: хэш не sha256", p)
		}
		if f.Size < 0 || f.Size > l.MaxFileBytes {
			return fmt.Errorf("%s: файл больше %d МБ", p, l.MaxFileBytes>>20)
		}
		total += f.Size
	}
	if total > l.MaxDeployBytes {
		return fmt.Errorf("сайт больше %d МБ", l.MaxDeployBytes>>20)
	}
	return nil
}

// Hashes are the distinct content hashes, sorted (descending, only to be stable).
func (m Manifest) Hashes() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range m {
		if !seen[f.Hash] {
			seen[f.Hash] = true
			out = append(out, f.Hash)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

// TotalBytes is the sum of the file sizes.
func (m Manifest) TotalBytes() int64 {
	var n int64
	for _, f := range m {
		n += f.Size
	}
	return n
}

// PathsOf are the paths that hold the content with this hash, sorted.
func (m Manifest) PathsOf(hash string) []string {
	var out []string
	for p, f := range m {
		if f.Hash == hash {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// ---- secrets ----

// Finding is a file that looks like it holds a secret. It never carries the secret itself.
type Finding struct {
	Path string `json:"path"`
	Rule string `json:"rule"`
}

var secretNames = regexp.MustCompile(`(?i)^(\.env(\..+)?|id_rsa|id_dsa|id_ecdsa|id_ed25519|.+\.(pem|key|p12|pfx|jks|keystore)|\.npmrc|\.netrc|\.pgpass)$`)
var envExamples = regexp.MustCompile(`(?i)^\.env\.(example|sample|template|dist|defaults)$`)

// ScanPaths finds the files whose names say "secret": .env, keys and certificates with private halves.
func ScanPaths(m Manifest) []Finding {
	var out []Finding
	for p := range m {
		base := path.Base(p)
		if envExamples.MatchString(base) || !secretNames.MatchString(base) {
			continue
		}
		out = append(out, Finding{Path: p, Rule: "имя файла похоже на секрет"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

var contentRules = []struct {
	name string
	re   *regexp.Regexp
}{
	{"приватный ключ", regexp.MustCompile(`-----BEGIN (?:[A-Z]+ )*PRIVATE KEY(?: BLOCK)?-----`)},
	{"ключ AWS", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"токен GitHub", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"токен GitLab", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"токен Slack", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`)},
	{"ключ Stripe", regexp.MustCompile(`\b[sr]k_live_[0-9A-Za-z]{24,}`)},
	{"ключ Anthropic", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{"токен Telegram-бота", regexp.MustCompile(`\b[0-9]{8,10}:AA[A-Za-z0-9_-]{33}\b`)},
}

// MaxScanBytes is how much of a file is read for secrets; the scan is for text, which sites are mostly made of.
const MaxScanBytes = 2 << 20

// ScanContent says which rule a file's content breaks, or "". Binary files (a NUL in the first bytes) are skipped.
func ScanContent(b []byte) string {
	if len(b) > MaxScanBytes {
		b = b[:MaxScanBytes]
	}
	head := b
	if len(head) > 1024 {
		head = head[:1024]
	}
	for _, c := range head {
		if c == 0 {
			return ""
		}
	}
	for _, r := range contentRules {
		if r.re.Match(b) {
			return r.name
		}
	}
	return ""
}
