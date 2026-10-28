// Package delivery is a file the agent hands to the person on purpose (`harnessly deliver <path>`, the MCP tool
// deliver_file): what the platform records of it, how the record travels in the tool's result, and how it reads.
//
// The record is a line of the tool's result, so the transcript keeps it: the feed finds it there (a card in the chat)
// and the pane's watcher finds it there (the file opens once), after a reload and after a restart alike. Files the agent
// only edits never make one: only the call that names the file does.
package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/files"
)

// Host is the address a record is written under: it never resolves, it only says what the line is.
const Host = "delivered.harnessly.invalid"

// MaxSumBytes is the biggest file whose sha256 is read; a bigger one is not delivered (it is not downloadable anyway).
const MaxSumBytes = files.MaxDownloadBytes

// Info is a delivered file.
type Info struct {
	// Path is the real, absolute path.
	Path   string
	Name   string
	Title  string
	Mime   string
	Size   int64
	SHA256 string
}

// Link writes the record as one address.
func Link(i Info) string {
	q := url.Values{}
	q.Set("path", i.Path)
	q.Set("name", i.Name)
	if i.Title != "" {
		q.Set("title", i.Title)
	}
	q.Set("mime", i.Mime)
	q.Set("size", strconv.FormatInt(i.Size, 10))
	q.Set("sha256", i.SHA256)
	return "https://" + Host + "/file?" + q.Encode()
}

// ParseLink reads a record back; ok is false for any other address or a record that lacks its path.
func ParseLink(link string) (Info, bool) {
	u, err := url.Parse(link)
	if err != nil || u.Host != Host || u.Path != "/file" {
		return Info{}, false
	}
	q := u.Query()
	size, err := strconv.ParseInt(q.Get("size"), 10, 64)
	i := Info{Path: q.Get("path"), Name: q.Get("name"), Title: q.Get("title"), Mime: q.Get("mime"), Size: size, SHA256: q.Get("sha256")}
	if err != nil || size < 0 || !path.IsAbs(i.Path) || i.Name == "" {
		return Info{}, false
	}
	return i, true
}

// LinkIn is the first record among links.
func LinkIn(links []string) (Info, bool) {
	for _, l := range links {
		if i, ok := ParseLink(l); ok {
			return i, true
		}
	}
	return Info{}, false
}

var mimes = map[string]string{
	"zip": "application/zip", "gz": "application/gzip", "tgz": "application/gzip", "tar": "application/x-tar",
	"7z": "application/x-7z-compressed", "pdf": "application/pdf",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"doc":  "application/msword", "xls": "application/vnd.ms-excel", "ppt": "application/vnd.ms-powerpoint",
	"csv": "text/csv", "json": "application/json", "txt": "text/plain", "md": "text/markdown", "markdown": "text/markdown",
	"html": "text/html", "htm": "text/html", "png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg",
	"gif": "image/gif", "webp": "image/webp", "svg": "image/svg+xml", "mp4": "video/mp4", "webm": "video/webm",
	"mov": "video/quicktime", "mp3": "audio/mpeg", "wav": "audio/wav",
}

// MimeOf is the type of a file by its extension; application/octet-stream for any other.
func MimeOf(name string) string {
	if m, ok := mimes[strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))]; ok {
		return m
	}
	return "application/octet-stream"
}

// Refusal is why a file is not delivered, in the words the agent reads.
type Refusal struct {
	Status int
	Text   string
}

func (r *Refusal) Error() string { return r.Text }

// Describe resolves a file under the access rules (the workspace's folders, no secrets, no links out of them), and reads
// its size and sha256. title is what the card is called ("" for the file's name).
func Describe(a *files.Access, requested, title string) (Info, *Refusal) {
	t := a.Download(requested)
	if !t.OK {
		return Info{}, &Refusal{Status: t.Status, Text: t.Text}
	}
	sum, err := sumOf(t.Real)
	if err != nil {
		return Info{}, &Refusal{Status: 404, Text: err.Error()}
	}
	name := path.Base(t.Real)
	return Info{Path: t.Real, Name: name, Title: strings.TrimSpace(title), Mime: MimeOf(name), Size: t.Size, SHA256: sum}, nil
}

// sumOf is the sha256 of the file's bytes, opened without following a link at its end.
func sumOf(real string) (string, error) {
	f, err := os.OpenFile(real, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return "", fmt.Errorf("the file cannot be read")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, MaxSumBytes+1)); err != nil {
		return "", fmt.Errorf("the file cannot be read")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Message is what the tool answers the agent: one line for the person's card (the record) and what to do next.
func Message(i Info) string {
	return "Delivered " + i.Name + ": the person sees it in the chat and can open or download it.\n" + Link(i)
}

// opensByItself: the pane shows these without being asked twice (a document, a page, a picture, an archive's contents); an
// office file, a media file or a binary is only downloaded, so the pane stays as the person left it.
var opensByItself = map[string]bool{
	"md": true, "markdown": true, "html": true, "htm": true, "pdf": true, "txt": true, "csv": true, "json": true,
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "svg": true, "zip": true,
}

// Opens says whether the pane opens the file at the moment it is handed over.
func (i Info) Opens() bool {
	return opensByItself[strings.ToLower(strings.TrimPrefix(path.Ext(i.Name), "."))]
}
