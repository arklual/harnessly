package filestore

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// The records of a library's entities: a design system, a video, a deck. The hub is where they live and a
// machine keeps a working copy; the embedded store of a runtime without a hub keeps the same records the same way, so
// one package (this one and the reader in internal/designrec) serves both. A collection holds one small JSON file per
// entity, `<id>.json`, named by the entity's project id in the design library; what the entity's files are stays in the
// design collection.
const (
	// KindSystems are the design systems (a project of kind ds).
	KindSystems = "systems"
	// KindVideos are the videos.
	KindVideos = "videos"
	// KindDecks are the slide decks.
	KindDecks = "decks"
	// KindProjects are the other projects of the library: a site, an app document (a kit is its own collection).
	KindProjects = "projects"
)

// RecordKinds are the collections of entity records, in a stable order.
var RecordKinds = []string{KindSystems, KindVideos, KindDecks, KindProjects}

// IsRecordKind tells whether a collection kind holds entity records.
func IsRecordKind(kind string) bool {
	for _, k := range RecordKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// MaxRecordBytes caps one record: it is a title, a kind and a pointer, not content.
const MaxRecordBytes = 64 << 10

var recordIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Records is the policy of the collections of entity records: a file `<id>.json` and nothing else.
type Records struct{}

func (Records) Synced(rel string) bool {
	id, ok := strings.CutSuffix(rel, ".json")
	return ok && SafeRel(rel) && recordIDRe.MatchString(id)
}

func (Records) SkippedDir(rel string) bool { return rel != "." && rel != "" }

// RecordPath is where the record of an entity is in its collection.
func RecordPath(id string) string { return id + ".json" }

// RecordID is the entity a path of a records collection is the record of.
func RecordID(rel string) (string, bool) {
	if !(Records{}).Synced(rel) {
		return "", false
	}
	return strings.TrimSuffix(rel, ".json"), true
}

// Record is what the store keeps of an entity: the facts the home page and the library list, not its files.
type Record struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Kind is the library's kind: design, deck, video, kit or app-doc.
	Kind    string `json:"kind"`
	Preview string `json:"preview,omitempty"`
	Kit     string `json:"kit,omitempty"`
	// Machine is the machine whose folder holds the working copy.
	Machine string `json:"machine,omitempty"`
	// At is when the facts last changed on the machine that wrote the record (Unix ms).
	At int64 `json:"at"`
}

// ParseRecord reads a record and checks it names the entity its path does.
func ParseRecord(raw []byte, id string) (Record, error) {
	var r Record
	if len(raw) > MaxRecordBytes {
		return r, errors.New("record too large")
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if r.ID != id {
		return r, errors.New("record names another entity")
	}
	return r, nil
}

// Marshal is the record as it is stored.
func (r Record) Marshal() ([]byte, error) { return json.Marshal(r) }

// KindCovers holds the cover picture of each project, `<id>.jpg`: the shot of its last check, kept in the store apart from
// the design's revisions (the machine's check folder is not part of the history). The hub gives it to the library under the
// access of the project.
const KindCovers = "covers"

// MaxCoverBytes caps one cover.
const MaxCoverBytes = 1 << 20

// Covers is the policy of the covers collection: a file `<id>.jpg` and nothing else.
type Covers struct{}

func (Covers) Synced(rel string) bool {
	id, ok := strings.CutSuffix(rel, ".jpg")
	return ok && SafeRel(rel) && recordIDRe.MatchString(id)
}

func (Covers) SkippedDir(rel string) bool { return rel != "." && rel != "" }

// CoverPath is where the cover of a project is in the covers collection.
func CoverPath(id string) string { return id + ".jpg" }

// CoverID is the project a path of the covers collection is the cover of.
func CoverID(rel string) (string, bool) {
	if !(Covers{}).Synced(rel) {
		return "", false
	}
	return strings.TrimSuffix(rel, ".jpg"), true
}
