package apps

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The files an app reaches: hl.files.read and hl.files.write, under the permissions files.read:<mask> and
// files.write:<mask>. The mask is a path pattern relative to the chat's workspace, and the permission is the whole of the
// app's reach: a path the masks do not name is refused, and so is one the host keeps from every app.
//
// A path is judged by where it really leads, not by how it is written. It is cleaned, resolved with every symlink
// followed, and the result has to lie inside the workspace (also resolved), match a mask, and not be a secret. The read or
// the write itself then goes through os.Root, which does not follow a link out of the workspace even when one is put in the
// way after the check, so a swap between the check and the open does not open a door.

// ErrFileRefused: the app may not reach this path.
var ErrFileRefused = errors.New("apps: the app may not reach this file")

// MaxFileBytes is the most one hl.files call moves.
const MaxFileBytes = 4 << 20

func refuseFile(format string, a ...any) error {
	return &BridgeError{Code: CodePermissionDenied, Message: fmt.Sprintf(format, a...), Detail: ErrFileRefused.Error()}
}

// secretNames are files of a workspace an app never reads or writes, whatever its masks say: environment files, keys and
// the tools' own credentials. A name with a * is matched by path.Match against each segment of the path.
var secretNames = []string{".env", ".env.*", ".npmrc", ".netrc", ".pypirc", ".git-credentials", ".htpasswd",
	"id_rsa", "id_rsa.*", "id_dsa", "id_ecdsa", "id_ed25519", "id_ed25519.*", "*.pem", "*.key", "*.p12", "*.pfx", "*.keystore",
	"cli-token", ".envrc",
	// The agents' own credentials and account (HRN-850): a chat in the home folder has them in its workspace.
	".credentials.json", ".claude.json",
	// What the person typed into shells and interpreters.
	".*_history", ".histfile", ".lesshst", ".viminfo"}

// secretPairs are a folder and a file in it that no app reaches: a file name too common to refuse everywhere.
var secretPairs = [][2]string{{".codex", "auth.json"}}

// secretDirs are folders of a workspace that are never reached: the repository's own files (config holds remotes with
// credentials, hooks run code), the shell's keys and the cloud tools' credentials.
var secretDirs = []string{".git", ".ssh", ".aws", ".gnupg", ".kube", ".docker", ".config", ".azure", ".password-store"}

// Tool and agent configuration is code or instructions in disguise (a hook, an MCP server, a setting, a prompt the agent
// reads): an app may read it but never write it. Dot-folders and dot-files in general are written only when a mask names
// them with a dot of its own (files.write:.notes/**), never through * or **.
var (
	writeOnlyNames = []string{".claude", ".mcp.json", ".vscode", ".idea", ".harnessly", ".github", ".cursor", ".cursorrules", ".windsurfrules", "copilot-instructions.md"}
	// writeOnlyPatterns are the agents' instruction files in all their spellings (CLAUDE.local.md, AGENTS.override.md).
	writeOnlyPatterns = []string{"claude*.md", "agents*.md", "gemini*.md"}
)

// FileScope is what one app, in one workspace, may reach.
type FileScope struct {
	// Workspace is the chat's working folder.
	Workspace string
	// Read and Write are the masks of the app's files.read:<mask> and files.write:<mask>.
	ReadMasks, WriteMasks []string
	// Forbidden are absolute folders no app reaches whatever the workspace holds: the host's data folder (the registry, the
	// cli-token), usually outside the workspace but not always.
	Forbidden []string
}

// FileScope is the app's reach into a workspace; forbidden are the host's own folders.
func (m *Manifest) FileScope(workspace string, forbidden ...string) FileScope {
	s := FileScope{Workspace: workspace, Forbidden: forbidden}
	for _, p := range m.Permissions {
		if mask, ok := strings.CutPrefix(p, "files.read:"); ok {
			s.ReadMasks = append(s.ReadMasks, mask)
		}
		if mask, ok := strings.CutPrefix(p, "files.write:"); ok {
			s.WriteMasks = append(s.WriteMasks, mask)
		}
	}
	return s
}

// CheckMask is "" for a good mask: a relative path of the workspace where * stands for a run of characters inside a
// segment and ** for any number of segments. A mask never leaves the workspace.
func CheckMask(mask string) string {
	switch {
	case mask == "":
		return "is empty"
	case strings.ContainsRune(mask, 0), strings.Contains(mask, "\\"):
		return "has a character a path does not"
	case strings.HasPrefix(mask, "/") || strings.HasPrefix(mask, "~") || filepath.VolumeName(mask) != "":
		return "is not relative to the workspace"
	}
	for _, seg := range strings.Split(mask, "/") {
		switch {
		case seg == "..":
			return "leaves the workspace (..)"
		case seg == "":
			return "has an empty segment"
		}
		if _, err := path.Match(seg, "x"); err != nil {
			return "is not a pattern: " + err.Error()
		}
		if strings.Contains(seg, "**") && seg != "**" {
			return "** stands as a segment of its own"
		}
	}
	return ""
}

// MatchMask says whether a clean relative path (slashes) is named by the mask.
func MatchMask(mask, rel string) bool {
	return matchSegments(strings.Split(mask, "/"), strings.Split(rel, "/"))
}

func matchSegments(mask, rel []string) bool {
	if len(mask) == 0 {
		return len(rel) == 0
	}
	if mask[0] == "**" {
		// ** is any number of segments, none included: docs/** names docs itself and all below it.
		for i := 0; i <= len(rel); i++ {
			if matchSegments(mask[1:], rel[i:]) {
				return true
			}
		}
		return false
	}
	if len(rel) == 0 {
		return false
	}
	if ok, _ := path.Match(mask[0], rel[0]); !ok {
		return false
	}
	return matchSegments(mask[1:], rel[1:])
}

func anyMask(masks []string, rel string) bool {
	for _, m := range masks {
		if MatchMask(m, rel) {
			return true
		}
	}
	return false
}

// secret says whether a clean relative path is, or lies in, one of the files and folders no app reaches.
func secret(rel string) bool {
	// Case-insensitive: on a case-insensitive volume .GIT/config is .git/config.
	segs := strings.Split(strings.ToLower(rel), "/")
	for i, seg := range segs {
		for _, pair := range secretPairs {
			if seg == pair[0] && i+1 < len(segs) && segs[i+1] == pair[1] {
				return true
			}
		}
		for _, d := range secretDirs {
			if seg == d {
				return true
			}
		}
		for _, n := range secretNames {
			if ok, _ := path.Match(n, seg); ok {
				return true
			}
		}
	}
	return false
}

// writeRefused says whether a write to a clean relative path is refused for what it is: agent and tool configuration, or a
// dot-file or dot-folder no mask names with a dot of its own.
func writeRefused(masks []string, rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		low := strings.ToLower(seg)
		for _, n := range writeOnlyNames {
			if low == n {
				return true
			}
		}
		for _, pat := range writeOnlyPatterns {
			if ok, _ := path.Match(pat, low); ok {
				return true
			}
		}
		if !strings.HasPrefix(seg, ".") {
			continue
		}
		named := false
		for _, m := range masks {
			for _, ms := range strings.Split(m, "/") {
				if ok, _ := path.Match(ms, seg); ok && strings.HasPrefix(ms, ".") {
					named = true
				}
			}
		}
		if !named {
			return true
		}
	}
	return false
}

// lexical turns what the view wrote into a clean path relative to the workspace, or refuses it: an absolute path, a ..,
// an empty path and a character a path does not hold never get further.
func lexical(rel string) (string, error) {
	switch {
	case rel == "" || len(rel) > 4096:
		return "", refuseFile("a path is a text of up to 4096 characters")
	case strings.ContainsRune(rel, 0) || strings.Contains(rel, "\\"):
		return "", refuseFile("the path has a character a path does not")
	case strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "~") || filepath.VolumeName(rel) != "":
		return "", refuseFile("a path is relative to the workspace, not absolute")
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", refuseFile("%q leaves the workspace", rel)
		}
		// Some filesystems read CLAUDE.md. or CLAUDE.md::$DATA as CLAUDE.md: a name with a colon, a control character or
		// a trailing dot or space is not a name an app may use.
		if strings.ContainsRune(seg, ':') || strings.HasSuffix(seg, ".") && seg != "." || strings.HasSuffix(seg, " ") ||
			strings.IndexFunc(seg, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return "", refuseFile("%q has a name the filesystem may read as another", rel)
		}
	}
	clean := path.Clean(rel)
	if clean == "." {
		return "", refuseFile("%q is the workspace itself, not a file", rel)
	}
	return clean, nil
}

// realPath resolves p with every symlink followed even where the last parts do not exist yet (a file about to be
// written): the deepest existing parent is resolved and the rest is put back after it.
func realPath(p string) (string, error) {
	rest := ""
	cur := p
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		// A link whose target is missing is not "not there": it is a link to follow, and the way out may start with it.
		if info, lerr := os.Lstat(cur); lerr == nil && info.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s is a link to nowhere", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// sameByIdentity says whether the forbidden folder (written as f, really fr) is the target (written, really real) or holds
// it or lies in it, by what the files are and not by how their names are spelled: on a volume that ignores case a
// different spelling still names the same folder, which os.SameFile sees whatever the volume's rules are.
func sameByIdentity(f, fr, written, real string) bool {
	for _, forbidden := range []string{f, fr} {
		fi, err := os.Stat(forbidden)
		if err != nil {
			continue
		}
		for _, target := range []string{written, real} {
			// The target and every folder above it that exists.
			for cur := target; ; cur = filepath.Dir(cur) {
				if ti, err := os.Stat(cur); err == nil && os.SameFile(fi, ti) {
					return true
				}
				if filepath.Dir(cur) == cur {
					break
				}
			}
		}
		// The target itself, when it is there, may hold the forbidden folder.
		for _, target := range []string{written, real} {
			ti, err := os.Stat(target)
			if err != nil {
				continue
			}
			for cur := forbidden; ; cur = filepath.Dir(cur) {
				if ci, err := os.Stat(cur); err == nil && os.SameFile(ci, ti) {
					return true
				}
				if filepath.Dir(cur) == cur {
					break
				}
			}
		}
	}
	return false
}

// within says whether p is dir or lies under it.
func within(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, strings.TrimRight(dir, string(filepath.Separator))+string(filepath.Separator))
}

// Resolve judges a path of the workspace for a read or a write and gives the clean relative path to use with the
// workspace's root. It does not touch the path's content.
func (s FileScope) Resolve(rel string, write bool) (string, error) {
	masks, verb := s.ReadMasks, "read"
	if write {
		masks, verb = s.WriteMasks, "write"
	}
	if len(masks) == 0 {
		return "", refuseFile("the app has no right to %s files (files.%s:<mask>)", verb, verb)
	}
	clean, err := lexical(rel)
	if err != nil {
		return "", err
	}
	if !anyMask(masks, clean) {
		return "", refuseFile("%q is not under a mask the app may %s", clean, verb)
	}
	if secret(clean) {
		return "", refuseFile("%q is a secret or the repository's own: no app reaches it", clean)
	}
	if write && writeRefused(masks, clean) {
		return "", refuseFile("%q is a tool's configuration or a hidden file no mask names: an app does not write it", clean)
	}
	if s.Workspace == "" || !filepath.IsAbs(s.Workspace) {
		return "", refuseFile("there is no workspace to reach files in")
	}
	root, err := filepath.EvalSymlinks(s.Workspace)
	if err != nil {
		return "", refuseFile("the workspace cannot be reached")
	}
	real, err := realPath(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return "", refuseFile("%q cannot be resolved", clean)
	}
	if !within(root, real) {
		return "", refuseFile("%q leads out of the workspace", clean)
	}
	for _, f := range s.Forbidden {
		if f == "" {
			continue
		}
		fr, err := realPath(f)
		if err != nil {
			fr = f
		}
		// Both the folder as it is written and as it really is.
		written := filepath.Join(root, filepath.FromSlash(clean))
		if within(fr, real) || within(real, fr) || within(f, written) || within(written, f) || sameByIdentity(f, fr, written, real) {
			return "", refuseFile("%q is the host's own", clean)
		}
	}
	// Where the path really leads is judged by the same rules: a link inside the workspace to a secret, or to a place the
	// masks do not name, is not a way round them.
	relReal, err := filepath.Rel(root, real)
	if err != nil || relReal == "." {
		return "", refuseFile("%q leads out of the workspace", clean)
	}
	relReal = filepath.ToSlash(relReal)
	if !anyMask(masks, relReal) {
		return "", refuseFile("%q leads to %q, which is not under a mask the app may %s", clean, relReal, verb)
	}
	if secret(relReal) {
		return "", refuseFile("%q leads to a secret or the repository's own: no app reaches it", clean)
	}
	if write && writeRefused(masks, relReal) {
		return "", refuseFile("%q leads to a tool's configuration or a hidden file no mask names", clean)
	}
	return clean, nil
}

// Read reads a file of the workspace the app may read, up to MaxFileBytes.
func (s FileScope) Read(rel string) ([]byte, error) {
	clean, err := s.Resolve(rel, false)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.Workspace)
	if err != nil {
		return nil, refuseFile("the workspace cannot be reached")
	}
	defer root.Close()
	f, err := root.Open(clean)
	if err != nil {
		return nil, fileErr(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fileErr(err)
	}
	if !info.Mode().IsRegular() {
		return nil, refuseFile("%q is not a plain file", clean)
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, fileErr(err)
	}
	if len(raw) > MaxFileBytes {
		return nil, &BridgeError{Code: CodeInvalidParams, Message: fmt.Sprintf("%q weighs more than %d MB", clean, MaxFileBytes>>20)}
	}
	return raw, nil
}

// Write writes a file of the workspace the app may write, creating the folders it lies in.
func (s FileScope) Write(rel string, data []byte) error {
	if len(data) > MaxFileBytes {
		return &BridgeError{Code: CodeInvalidParams, Message: fmt.Sprintf("a file is at most %d MB", MaxFileBytes>>20)}
	}
	clean, err := s.Resolve(rel, true)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Workspace)
	if err != nil {
		return refuseFile("the workspace cannot be reached")
	}
	defer root.Close()
	if dir := path.Dir(clean); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return fileErr(err)
		}
	}
	f, err := root.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fileErr(err)
	}
	_, werr := f.Write(data)
	return fileErr(errors.Join(werr, f.Close()))
}

// fileErr turns an error of the file system into what a view may read: that the path is not there or not allowed, no
// more (the text of an OS error names the host's folders).
func fileErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return &BridgeError{Code: CodeInvalidParams, Message: "there is no such file"}
	case errors.Is(err, fs.ErrPermission):
		return refuseFile("the file system refuses it")
	}
	return &BridgeError{Code: CodeInvalidParams, Message: "the file cannot be used", Detail: err.Error()}
}
