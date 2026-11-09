// Package lib holds what the hub and the agent must agree on: which paths of the library sync at all.
package filestore

import (
	"path"
	"regexp"
	"strings"
)

// TmpDir is where the agent stages a downloaded file before it renames it into place, at the library's root.
const TmpDir = ".sync-tmp"

// MaxFileSize is the biggest file that syncs. GitLab refuses bigger ones in the backup, and nothing a board needs is
// that big.
const MaxFileSize = 50 << 20

// OwnerFile is the one hidden file that syncs: `<project>/.owner.json`, who made the project. Without it
// a personal design is invisible to its owner on every machine but the one that made it. It is safe where other
// hidden entries are not because it is plain data that no tool executes or loads as configuration (unlike .claude/
// hooks, .mcp.json or .envrc), and it is allowed only as a file directly in the project folder, never deeper and
// never as a folder, so it cannot be used to carry anything else past the hidden-path rule.
const OwnerFile = ".owner.json"

// FoldersDir holds each person's shelf of the library: `.folders/<32 hex>.json`, their folders and which
// project lies in which, named by a hash of their address. It syncs so the folders a person makes on one machine are
// there on every machine that shows the library; like OwnerFile it is plain data no tool loads, and only that exact
// name syncs, never anything else under the folder.
const FoldersDir = ".folders"

var layoutFile = regexp.MustCompile(`^\.folders/[0-9a-f]{32}\.json$`)

// MediaDir is a project's hidden folder of machine state (recordings, exports, thumbnails). Two things in it are the
// design's and sync: the folder guests read the published files from (`<project>/.media/published/<file>`:
// the deck's PDF, a video's MP4, its chapters — files the runtime made, plain data no tool loads, one level deep), and
// the mark that a deck's PDF is kept for guests (`<project>/.media/pdf-auto`), so any machine of the space may build it.
const MediaDir = ".media"

// publishedNames are the files a runtime publishes for guests: only these sync, never another name put there.
var publishedNames = map[string]bool{
	"deck.pdf": true, "deck.pdf.rev": true, "chapters.json": true, "video.json": true, "video.mp4": true, "video-9x16.mp4": true,
}

// publishedFile: a published file or the PDF mark of a project (parts of a path that passed the general checks).
func publishedFile(parts []string) bool {
	switch {
	case len(parts) == 3 && parts[1] == MediaDir && parts[2] == "pdf-auto":
		return true
	case len(parts) == 4 && parts[1] == MediaDir && parts[2] == "published":
		return publishedNames[parts[3]]
	}
	return false
}

// Folders that belong to the machine, not to the design, that do not start with a dot. Everything hidden is the
// machine's too (see Synced).
var skipped = map[string]bool{
	"node_modules": true,
}

// Instructions for agents (CLAUDE.md, AGENTS.md, GEMINI.md and their variants). Synced, they would let one machine's
// agent steer the agents of another; a design never needs them.
var agentNotes = regexp.MustCompile(`(?i)^(claude|agents|gemini)[^/]*\.md$`)

// Claude Code writes a file as `<name>.tmp.<pid>.<ms>` and renames it over the target; editors leave `~` and swap
// files. None of them is a design.
var scratch = regexp.MustCompile(`(\.tmp\.\d+\.\d+|~|\.swp|\.swx)$`)

// Synced tells whether a slash-separated path relative to the library's root is a file that syncs.
//
// Only files inside a project folder sync: the root's own files (the agents' CLAUDE.md, `.template/`) are written by
// each machine's runtime, and two runtimes of different versions would otherwise overwrite each other forever.
// Copies kept from earlier migrations (`*.before-tck-*`) stay on their machine.
func Synced(rel string) bool {
	if rel == "" || len(rel) > 1024 || strings.ContainsAny(rel, "\x00\\\n\r") {
		return false
	}
	if strings.HasPrefix(rel, "/") || path.Clean(rel) != rel {
		return false
	}
	if layoutFile.MatchString(rel) {
		return true
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return false
	}
	if strings.HasPrefix(parts[0], ".") || strings.Contains(parts[0], ".before-tck-") {
		return false
	}
	if len(parts) == 2 && parts[1] == OwnerFile {
		return true
	}
	if publishedFile(parts) && !skipped[parts[0]] && parts[0] != "" {
		return true
	}
	for _, part := range parts {
		// Hidden entries are machine state or tool settings, never design: claims, trash, thumbnails, check
		// screenshots, our staging folder, .git, and settings that make a tool run commands (.claude/ hooks,
		// .mcp.json, .envrc). A denylist of names would miss the next one.
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || skipped[part] {
			return false
		}
	}
	name := parts[len(parts)-1]
	return !scratch.MatchString(name) && !agentNotes.MatchString(name)
}

// SkippedDir tells whether a folder (slash path relative to the root) holds nothing that syncs, so a walk or a
// watch need not enter it.
func SkippedDir(rel string) bool {
	if rel == "." || rel == "" || rel == FoldersDir {
		return false
	}
	parts := strings.Split(rel, "/")
	if strings.HasPrefix(parts[0], ".") || strings.Contains(parts[0], ".before-tck-") {
		return true
	}
	// The way to the published files: the media folder and its published folder, nothing else of it.
	if len(parts) == 2 && parts[1] == MediaDir || len(parts) == 3 && parts[1] == MediaDir && parts[2] == "published" {
		return skipped[parts[0]]
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") || skipped[part] {
			return true
		}
	}
	return false
}

// ValidSha tells whether s is a lowercase hex sha256.
func ValidSha(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
