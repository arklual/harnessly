package hubproto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"time"
)

// A person's agent memory, kept in the hub so that every machine of the person sees the same notes (docs/hub/memory-sync.md).
// The machine signs each request with its key under MemoryDomain; the hub, not the request, says whose memory it is.

// MemoryDomain is the first line of the message signed to read or write the memory of the machine's person.
const MemoryDomain = "harnessly-agent-memory-v1"

// The hub's memory endpoints.
const (
	MemoryPullPath = "/agent/memory/pull"
	MemoryPutPath  = "/agent/memory/put"
)

// Limits of the memory a hub keeps for a person.
const (
	// MemoryMaxNote is the largest note, in bytes.
	MemoryMaxNote = 256 << 10
	// MemoryMaxNotes is the most notes of one project.
	MemoryMaxNotes = 1000
	// MemoryMaxProjects is the most projects one pull asks for.
	MemoryMaxProjects = 200
	// MemoryMaxProject is the longest project id.
	MemoryMaxProject = 300
)

// MemoryNote is one note as the hub keeps it. Body is the whole file, front matter included. A deleted note stays as a
// tombstone (Deleted, no body) so that the other machines learn of the delete instead of writing the note back.
type MemoryNote struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Body    string `json:"body,omitempty"`
	Version string `json:"version"`
	Deleted bool   `json:"deleted,omitempty"`
	// Machine is the name of the machine that wrote it last; UpdatedAt is when, in Unix milliseconds.
	Machine   string `json:"machine,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

// MemoryPull is the body of POST /agent/memory/pull: the projects whose notes the machine wants.
type MemoryPull struct {
	Projects []string `json:"projects"`
}

// MemoryNotes is the answer of a pull: every note of those projects, tombstones included.
type MemoryNotes struct {
	Notes []MemoryNote `json:"notes"`
}

// MemoryPut is the body of POST /agent/memory/put: write (or, with Delete, remove) one note. Base is the version the
// machine last saw ("" for a note it never saw): a note that changed since is not overwritten, the answer carries it
// as Conflict instead.
type MemoryPut struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Body    string `json:"body,omitempty"`
	Delete  bool   `json:"delete,omitempty"`
	Base    string `json:"base"`
}

// MemoryPutResult is the answer of a put: the note as kept now, or the newer note that stopped the write.
type MemoryPutResult struct {
	Note     *MemoryNote `json:"note,omitempty"`
	Conflict *MemoryNote `json:"conflict,omitempty"`
}

// MemoryVersion is the version of a note's text: the same text is the same version on every machine.
func MemoryVersion(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:12])
}

// MemoryTombstone is the version of a deleted note.
const MemoryTombstone = "deleted"

// SignMemory makes the Authorization header of a machine's memory request: the shape of a dial's, under MemoryDomain.
func SignMemory(priv ed25519.PrivateKey, machineID string, now time.Time, host string) (string, error) {
	return signHeader(MemoryDomain, priv, machineID, now, host)
}

// VerifyMemory checks a header made by SignMemory.
func VerifyMemory(pub ed25519.PublicKey, a DialAuth, host string, now time.Time) error {
	return verifyHeader(MemoryDomain, pub, a, host, now)
}

var memoryNoteName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,199}\.md$`)

// MemoryNameOK says whether name is a note's file name (letters, digits, dot, dash, underscore, ending in .md; no
// leading dot).
func MemoryNameOK(name string) bool { return memoryNoteName.MatchString(name) }

// MemoryProjectOK says whether p is a project id a hub keeps: printable, without spaces, at most MemoryMaxProject long.
func MemoryProjectOK(p string) bool {
	if p == "" || len(p) > MemoryMaxProject {
		return false
	}
	for _, r := range p {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}
