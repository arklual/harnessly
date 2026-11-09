package datamove

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Kinds of the move: the store collections the objects go to (filestore.KindUploads, KindFiles, KindHandoff).
const (
	KindUploads = "uploads"
	KindFiles   = "files"
	KindHandoff = "handoff"
)

// Uploads are the files people put into chats: <dir>/<id> with its AttachmentRef in <dir>/<id>.json (wire.Uploads).
type Uploads struct{ Dir string }

func (Uploads) Kind() string { return KindUploads }

func (u Uploads) List(ctx context.Context) ([]Item, error) {
	refs, err := filepath.Glob(filepath.Join(u.Dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, f := range refs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var ref struct{ ID, Name, Mime string }
		if json.Unmarshal(raw, &ref) != nil || ref.ID == "" || ref.ID+".json" != filepath.Base(f) {
			continue
		}
		out = append(out, Item{Kind: KindUploads, Object: ref.ID, Path: u.blob(ref.ID), Mime: ref.Mime, Name: ref.Name})
	}
	return out, nil
}

// blob is the file that holds an upload's bytes: <id><ext> since uploads carry their type's extension (wire.uploadExt),
// <id> for one from before. The bare name stands when neither is on the disk, so the move reports it gone.
func (u Uploads) blob(id string) string {
	bare := filepath.Join(u.Dir, id)
	named, _ := filepath.Glob(bare + ".*")
	for _, p := range named {
		if !strings.HasSuffix(p, ".json") {
			if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
				return p
			}
		}
	}
	return bare
}

// Handoffs are the handoff and merge notes (internal/handoff): <dir>/drafts/… and <dir>/claimed/…, *.jsonl.
type Handoffs struct{ Dir string }

func (Handoffs) Kind() string { return KindHandoff }

func (h Handoffs) List(ctx context.Context) ([]Item, error) {
	var out []Item
	err := filepath.WalkDir(h.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		rel, err := filepath.Rel(h.Dir, p)
		if err != nil {
			return nil
		}
		out = append(out, Item{Kind: KindHandoff, Object: filepath.ToSlash(rel), Path: p})
		return nil
	})
	return out, err
}

// Delivered are the files the agents handed over (`harnessly deliver`): each left a record (delivery.Link) in its
// chat's transcript with the path and the sha256 of the bytes it showed. Transcripts are read once each, by size and
// time, at a pace that leaves the disk to the agents.
type Delivered struct {
	// Transcripts lists the transcripts of the runtime's chats.
	Transcripts func(ctx context.Context) []string
	// Pause is the rest after each transcript read (20 ms).
	Pause time.Duration

	seen map[string]scanned
}

type scanned struct {
	size, mtime int64
	items       []Item
}

func (*Delivered) Kind() string { return KindFiles }

var (
	toolResult = []byte(`"tool_result"`)
	callOutput = []byte(`"function_call_output"`)
)

// scanRate is how many bytes of transcripts a second the inventory reads at most.
const scanRate = 32 << 20

var deliveredRe = regexp.MustCompile(`https://` + regexp.QuoteMeta(delivery.Host) + `/file\?[^\s"'\\<>)\]]+`)

func (d *Delivered) List(ctx context.Context) ([]Item, error) {
	if d.Transcripts == nil {
		return nil, nil
	}
	if d.seen == nil {
		d.seen = map[string]scanned{}
	}
	pause := d.Pause
	if pause <= 0 {
		pause = 20 * time.Millisecond
	}
	bySha := map[string]Item{}
	for _, t := range d.Transcripts(ctx) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		fi, err := os.Stat(t)
		if err != nil {
			continue
		}
		s, ok := d.seen[t]
		if !ok || s.size != fi.Size() || s.mtime != fi.ModTime().UnixMilli() {
			items, err := scanTranscript(t)
			if err != nil {
				continue
			}
			s = scanned{size: fi.Size(), mtime: fi.ModTime().UnixMilli(), items: items}
			d.seen[t] = s
			// At most scanRate bytes a second over the transcripts, so a machine with gigabytes of them keeps its
			// disk for its agents while the move reads.
			rest := pause
			if byRate := time.Duration(float64(fi.Size()) / scanRate * float64(time.Second)); byRate > rest {
				rest = byRate
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(rest):
			}
		}
		for _, it := range s.items {
			bySha[it.Object] = it
		}
	}
	out := make([]Item, 0, len(bySha))
	for _, it := range bySha {
		out = append(out, it)
	}
	return out, nil
}

// scanTranscript finds the records of handed-over files in one transcript: only lines that name the record's host are
// parsed; JSON may have written the record's ampersands as \u0026.
func scanTranscript(path string) ([]Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	host := []byte(delivery.Host)
	r := bufio.NewReaderSize(f, 1<<20)
	var out []Item
	for {
		line, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A line longer than the buffer: read it whole only when it is one of the records.
			whole := append([]byte(nil), line...)
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = r.ReadSlice('\n')
				whole = append(whole, line...)
			}
			line = whole
		}
		// Only what a tool answered: the record `harnessly deliver` writes comes back as the result of its call
		// (Claude's tool_result, Codex's function_call_output); a link a person or an agent typed is not one.
		if bytes.Contains(line, host) && (bytes.Contains(line, toolResult) || bytes.Contains(line, callOutput)) {
			text := strings.ReplaceAll(string(line), `\u0026`, "&")
			for _, link := range deliveredRe.FindAllString(text, -1) {
				i, ok := delivery.ParseLink(link)
				if ok && len(i.SHA256) == 64 {
					out = append(out, Item{Kind: KindFiles, Object: i.SHA256, Path: i.Path, Sha256: i.SHA256, Mime: i.Mime, Name: i.Name})
				}
			}
		}
		if err != nil {
			return out, nil
		}
	}
}

// KindSidebar is the machine's sidebar (hubproto.MigrationSidebarKind): its projects and favorites, merged into its
// person's sidebar on the hub, once and then whenever a project is new.
const KindSidebar = hubproto.MigrationSidebarKind

// Sidebar is the machine's projects and favorites. Read returns them (nil: nothing to merge); Send merges them into the
// person's sidebar and says whether the hub lists the machine as merged (ErrNotKept: the hub keeps no sidebar).
type Sidebar struct {
	Read func(ctx context.Context) (*hubproto.MigrationSidebar, error)
	Send func(ctx context.Context, side hubproto.MigrationSidebar) (bool, error)
}

func (Sidebar) Kind() string { return KindSidebar }

func (s Sidebar) List(ctx context.Context) ([]Item, error) {
	side, err := s.Read(ctx)
	if err != nil || side == nil {
		return nil, err
	}
	if len(side.Projects) == 0 && (side.Favorites == nil || len(side.Favorites.Groups) == 0 && len(side.Favorites.Loose) == 0) {
		return nil, nil
	}
	data, err := json.Marshal(side)
	if err != nil {
		return nil, err
	}
	send := *side
	return []Item{{Kind: KindSidebar, Object: hubproto.MigrationSidebarObject, Name: "sidebar", Data: data,
		Deliver: func(ctx context.Context, _ []byte) error {
			merged, err := s.Send(ctx, send)
			if err == nil && !merged {
				err = errors.New("the hub took the sidebar and does not list the machine as merged")
			}
			return err
		}}}, nil
}
