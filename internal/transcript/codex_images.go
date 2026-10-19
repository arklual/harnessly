package transcript

import (
	"path/filepath"
	"strings"
)

var codexImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// nodeExtname is path.extname: from the last dot of the base name, none for a leading dot or "..".
func nodeExtname(p string) string {
	base := p[strings.LastIndexByte(p, '/')+1:]
	i := strings.LastIndexByte(base, '.')
	if i <= 0 || base == ".." {
		return ""
	}
	return base[i:]
}

// encodeURIComponent escapes all but A-Z a-z 0-9 - _ . ! ~ * ' ( ).
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// NewCodexImageURL is the ImageURL of a runtime that serves the images under uploadsDir at
// <prefix>/api/uploads/<path below uploadsDir> (uploadPathToImageUrl): "" for a path outside it or not an image.
func NewCodexImageURL(uploadsDir, prefix string) func(string) string {
	root, err := filepath.Abs(uploadsDir)
	if err != nil {
		root = filepath.Clean(uploadsDir)
	}
	return func(p string) string {
		if strings.ContainsRune(p, 0) {
			return ""
		}
		resolved, err := filepath.Abs(p)
		if err != nil || resolved == root || !strings.HasPrefix(resolved, root+"/") {
			return ""
		}
		if !codexImageExts[strings.ToLower(nodeExtname(resolved))] {
			return ""
		}
		var segs []string
		for _, s := range strings.Split(resolved[len(root)+1:], "/") {
			if s != "" {
				segs = append(segs, encodeURIComponent(s))
			}
		}
		if len(segs) == 0 {
			return ""
		}
		return prefixAppPath("/api/uploads/"+strings.Join(segs, "/"), prefix)
	}
}

// prefixAppPath puts the app's mount point before a path of the app that lacks it.
func prefixAppPath(value, base string) string {
	if base == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || value == base ||
		strings.HasPrefix(value, base+"/") {
		return value
	}
	return base + value
}
