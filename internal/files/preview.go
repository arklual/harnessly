package files

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Files the agent produced or read, opened in the viewer or handed over to be saved.

const (
	MaxPreviewBytes  = 15 * 1024 * 1024
	MaxVideoBytes    = 500 * 1024 * 1024
	MaxDownloadBytes = 500 * 1024 * 1024
)

// VideoTypes are read in ranges.
var VideoTypes = map[string]string{"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "mov": "video/quicktime"}

// PreviewTypes are what the viewer shows, by extension.
var PreviewTypes = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp", "svg": "image/svg+xml",
	"pdf": "application/pdf", "html": "text/html; charset=utf-8", "htm": "text/html; charset=utf-8",
	"md": "text/markdown; charset=utf-8", "markdown": "text/markdown; charset=utf-8",
	"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "mov": "video/quicktime",
}

// inheritedTypes are extensions the old runtime's type tables answered from their object prototype: such a file
// passes as a video, and its content type is the prototype's member written as text.
var inheritedTypes = map[string]string{"constructor": "function Object() { [native code] }", "__proto__": "[object Object]"}

// PreviewType is the content type the viewer serves a file of this extension with, and whether it is read in ranges.
func PreviewType(ext string) (contentType string, video bool) {
	if t, ok := inheritedTypes[ext]; ok {
		return t, true
	}
	return PreviewTypes[ext], VideoTypes[ext] != ""
}

// Access says which files may be opened: under the roots, and not secret-looking.
type Access struct {
	Roots  []string
	secret *regexp.Regexp
}

// NewAccess builds the rules. roots are the folders files may be in (made absolute and clean); configDirs are more
// home-relative folders that hold credentials (".config/<app>"), refused like ".ssh".
func NewAccess(roots, configDirs []string) *Access {
	a := &Access{}
	for _, r := range roots {
		if r == "" {
			continue
		}
		a.Roots = append(a.Roots, resolveAbs(r))
	}
	alts := []string{`\.env(\..*)?`, `\.ssh`, `\.gnupg`, `\.aws`}
	for _, d := range configDirs {
		if d = strings.Trim(d, "/"); d != "" {
			alts = append(alts, regexp.QuoteMeta(d))
		}
	}
	alts = append(alts, `\.credentials\.json`, `\.netrc`, `id_[a-z0-9]+`, `.*\.pem`, `.*\.key`)
	a.secret = regexp.MustCompile(asciiFold(`(^|/)(` + strings.Join(alts, "|") + `)(/|$)`))
	return a
}

// resolveAbs is an absolute, clean path; a relative one is taken from the process's folder.
func resolveAbs(p string) string {
	if !strings.HasPrefix(p, "/") {
		wd, _ := os.Getwd()
		p = wd + "/" + p
	}
	return filepath.Clean(p)
}

// Secret: the path looks like it holds credentials.
func (a *Access) Secret(p string) bool { return a.secret.MatchString(p) }

func (a *Access) allowed(real string) bool {
	for _, root := range a.Roots {
		if real == root || strings.HasPrefix(real, root+"/") {
			return true
		}
	}
	return false
}

// Target is a file that may be served, or the refusal.
type Target struct {
	OK     bool
	Real   string
	Ext    string
	Size   int64
	Status int
	Text   string
}

func refuse(status int, text string) Target { return Target{Status: status, Text: text} }

func lastDotExt(p string) string {
	return strings.ToLower(p[strings.LastIndexByte(p, '.')+1:])
}

func (a *Access) locate(requested string) (string, *Target) {
	if !strings.HasPrefix(requested, "/") || strings.Contains(requested, "\x00") {
		t := refuse(400, locale.Pick("An absolute path is required", "Нужен абсолютный путь"))
		return "", &t
	}
	real, err := Realpath(requested)
	if err != nil {
		t := refuse(404, locale.Pick("File not found", "Файл не найден"))
		return "", &t
	}
	if !a.allowed(real) || a.Secret(real) {
		t := refuse(403, locale.Pick("This file cannot be opened from the workspace", "Этот файл нельзя открыть из рабочего пространства"))
		return "", &t
	}
	return real, nil
}

// Preview resolves a file the viewer may show: an existing picture, video or document under the roots.
func (a *Access) Preview(requested string) Target {
	real, refused := a.locate(requested)
	if refused != nil {
		return *refused
	}
	ext := lastDotExt(real)
	contentType, video := PreviewType(ext)
	if contentType == "" {
		return refuse(415, locale.Pick("Preview exists only for pictures, video, md, html and pdf", "Просмотр есть только для картинок, видео, md, html и pdf"))
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return refuse(404, locale.Pick("File not found", "Файл не найден"))
	}
	limit := int64(MaxPreviewBytes)
	if video {
		limit = MaxVideoBytes
	}
	if st.Size() > limit {
		return refuse(413, locale.Pick("The file is too large to preview", "Файл слишком большой для просмотра"))
	}
	return Target{OK: true, Real: real, Ext: ext, Size: st.Size()}
}

// Download resolves a file the agent sent to be saved: any type, the same places, secrets kept out.
func (a *Access) Download(requested string) Target {
	real, refused := a.locate(requested)
	if refused != nil {
		return *refused
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return refuse(404, locale.Pick("File not found", "Файл не найден"))
	}
	if st.Size() > MaxDownloadBytes {
		return refuse(413, locale.Pick("The file is too large", "Файл слишком большой"))
	}
	return Target{OK: true, Real: real, Ext: lastDotExt(real), Size: st.Size()}
}

var rangePattern = regexp.MustCompile(`^bytes=(\d*)-(\d*)$`)

// number reads digits as the old runtime's numbers did: a double, so a huge value stays huge instead of failing.
func number(digits string) float64 {
	f, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return math.Inf(1)
	}
	return f
}

// PreviewRange is the byte range of a video the viewer asks for: "bytes=a-b", "bytes=a-" or "bytes=-n"; ok is false
// when the header asks for nothing that can be served.
func PreviewRange(header string, size int64) (start, end int64, ok bool) {
	m := rangePattern.FindStringSubmatch(trimSpace(header))
	if m == nil || m[1] == "" && m[2] == "" || size == 0 {
		return 0, 0, false
	}
	n := float64(size)
	if m[1] == "" {
		tail := math.Min(number(m[2]), n)
		if tail > 0 {
			return int64(n - tail), size - 1, true
		}
		return 0, 0, false
	}
	s := number(m[1])
	e := n - 1
	if m[2] != "" {
		e = math.Min(number(m[2]), n-1)
	}
	if s <= e && s < n {
		return int64(s), int64(e), true
	}
	return 0, 0, false
}

// ASCIIName is a file name safe inside a quoted header parameter: every UTF-16 unit outside printable ASCII, and
// quotes and backslashes, become "_".
func ASCIIName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\':
			sb.WriteByte('_')
		case r >= 0x20 && r <= 0x7e:
			sb.WriteRune(r)
		case r > 0xFFFF:
			sb.WriteString("__")
		default:
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

// PreviewCSP is the policy a shown document runs under (all but PDFs and videos).
const PreviewCSP = "sandbox allow-scripts; default-src 'none'; img-src data: blob: https:; style-src 'unsafe-inline' https:; font-src https: data:; script-src 'unsafe-inline' https:"
