// Package memstore is the contract of the agent's memory store: list the notes, read one, write one, search by text.
// The default implementation is the files Claude Code and harnessly already keep (FileStore), and nothing changes for a
// person without an adapter. A corporate store (a wiki, a database) connects as an adapter of the extension frame
// (internal/hub/ext), protocol harnessly.memory/v1 (docs/memory-adapters.md), and is layered over the files.
package memstore

import (
	"context"
	"errors"
)

// Errors a store answers.
var (
	// ErrNotFound: there is no such note.
	ErrNotFound = errors.New("memstore: no such note")
	// ErrUnsupported: the store does not do this (a read-only store asked to write, an adapter without search).
	ErrUnsupported = errors.New("memstore: not supported")
	// ErrConflict: the note changed since the version the writer read.
	ErrConflict = errors.New("memstore: the note changed")
	// ErrBadName: the name is not a note name (letters, digits, dot, dash, underscore, ending in .md).
	ErrBadName = errors.New("memstore: bad note name")
	// ErrUnreachable: an external store did not answer.
	ErrUnreachable = errors.New("memstore: the store is unreachable")
)

// Entry is a note without its text.
type Entry struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// ModifiedMs is when the note last changed, milliseconds since the epoch.
	ModifiedMs float64 `json:"modifiedMs"`
	// Version changes with every write; Put with IfVersion refuses to overwrite a newer one.
	Version string `json:"version"`
}

// Note is an entry with its text, front matter taken off.
type Note struct {
	Entry
	Body string `json:"body"`
}

// Hit is a search result.
type Hit struct {
	Entry
	// Snippet is a line of the note around the match.
	Snippet string `json:"snippet"`
}

// Store is the memory contract.
type Store interface {
	// List is every note, by name.
	List(ctx context.Context) ([]Entry, error)
	// Get reads one note; ErrNotFound when there is none.
	Get(ctx context.Context, name string) (Note, error)
	// Put writes a note. ifVersion, when not empty, is the version the writer read: a newer note is ErrConflict. It
	// answers the new entry.
	Put(ctx context.Context, name, text, ifVersion string) (Entry, error)
	// Search finds notes whose title or text holds the query (case-insensitive), at most limit of them (0: 20).
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
}
