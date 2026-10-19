package files

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Attachments people uploaded into a conversation, served back to the page by their path under the uploads folder.

var servedTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".m4v": "video/x-m4v", ".ogv": "video/ogg",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".m4a": "audio/mp4",
	".aac": "audio/aac", ".flac": "audio/flac", ".opus": "audio/ogg", ".weba": "audio/webm",
	".pdf": "application/pdf",
}

// UploadType is the content type an upload is served with, "" for a type that is not served.
func UploadType(p string) string { return servedTypes[strings.ToLower(ExtName(p))] }

// UploadPath is the file the segments name inside dir, or ok false: no segments, an empty, ".", ".." or NUL segment,
// a segment with a slash or backslash, or a path that is the folder itself or outside it. A link inside is followed
// when the file is read only while it stays inside the folder (ServeUpload).
func UploadPath(dir string, segments []string) (string, bool) {
	if len(segments) == 0 {
		return "", false
	}
	for _, s := range segments {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "\x00/\\") {
			return "", false
		}
	}
	root := resolveAbs(dir)
	resolved := filepath.Join(append([]string{root}, segments...)...)
	if resolved == root || !strings.HasPrefix(resolved, root+"/") {
		return "", false
	}
	return resolved, true
}

var uploadRangePattern = regexp.MustCompile(`^bytes=(\d*)-(\d*)$`)

// UploadRange is the single byte range a media element asks for. present is false when there is no range to answer
// (no header, or not a single range); satisfiable is false for a range outside the file.
func UploadRange(header string, size int64) (start, end int64, present, satisfiable bool) {
	if header == "" {
		return 0, 0, false, false
	}
	m := uploadRangePattern.FindStringSubmatch(trimSpace(header))
	if m == nil || m[1] == "" && m[2] == "" {
		return 0, 0, false, false
	}
	n := float64(size)
	var s, e float64
	if m[1] == "" {
		s = max(0, n-number(m[2]))
	} else {
		s = number(m[1])
	}
	if m[1] == "" || m[2] == "" {
		e = n - 1
	} else {
		e = min(number(m[2]), n-1)
	}
	if s > e || s >= n {
		return 0, 0, true, false
	}
	return int64(s), int64(e), true, true
}

// openByExtension: paths the old runtime let through without a session because they look like a static asset.
var openByExtension = regexp.MustCompile(`^/` + jsDotClass + `*\.(?:svg|png|ico|jpg|jpeg|webp|webmanifest|ttf|woff|woff2)`)

// OpenWithoutSession: a path served to anyone. An extension opens only a page asset, never an API path: the old
// rule let /api/uploads/<anything>.png through without a session (closed in both runtimes).
func OpenWithoutSession(apiPath string) bool {
	return !strings.HasPrefix(apiPath, "/api/") && openByExtension.MatchString(apiPath)
}
