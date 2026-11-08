package filesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// A write by revision. The hub takes a file's write only on top of the revision the writer started from.
// A writer that lost the race used to leave "Main (conflict vm-b ….dc.html)" beside the board; nobody read
// them. Now the hub's version takes the board's place and the losing edit is neither lost nor a copy in the way:
//
//   - kept: the losing contents are put into KeptDir (hidden, never synced) with a notice beside them. The runtime of
//     the machine reads the notices (`<KeptDir>/<path>.<machine>.<time>.json`) and tells the agent that wrote;
//   - reapply: a board whose edit is small (kindReapply) is only kept: the agent is told to re-apply it on the fresh
//     version, it still has what it wrote;
//   - variant: a bigger edit of a board is also saved as a project of its own, `<project>-variant-…`, with
//     `variant.json` naming the original (the link record the variants are built on).
//
// An agent of an older version keeps making conflict copies; the hub does not care.

// KeptDir is where a losing edit is kept, relative to the folder's root; hidden, so it is never synced.
const KeptDir = ".conflicts"

// Notice kinds.
const (
	KindKept    = "kept"    // a file that is not a board: the losing contents are kept
	KindReapply = "reapply" // a small edit of a board: re-apply it on the fresh version
	KindVariant = "variant" // a bigger edit of a board: saved as the project Variant
)

// smallEditBytes is how much two versions of a board may differ (the changed middle of both together) for the edit to
// be called small.
const smallEditBytes = 4096

// VariantFile is the link record of a variant project, at its root.
const VariantFile = "variant.json"

// Notice is what the machine keeps about a losing edit, next to the contents.
type Notice struct {
	V    int    `json:"v"`
	Kind string `json:"kind"`
	// Path is the file whose write lost, Kept where its contents are (relative to the root).
	Path string `json:"path"`
	Kept string `json:"kept"`
	// Project and Board name a board (<project>/project/<board>.dc.html); empty for another file.
	Project string `json:"project,omitempty"`
	Board   string `json:"board,omitempty"`
	// Variant is the project the edit was saved as (kind variant).
	Variant string `json:"variant,omitempty"`
	// CurrentRev, By and ByMachine tell whose version took the place.
	CurrentRev int64  `json:"current_rev"`
	By         string `json:"by,omitempty"`
	ByMachine  string `json:"by_machine,omitempty"`
	Machine    string `json:"machine"`
	At         string `json:"at"`
	Bytes      int    `json:"bytes"`
}

// VariantLink is the record `variant.json` of a variant project: whose variant it is, of which board and revision.
type VariantLink struct {
	V       int    `json:"v"`
	Of      string `json:"of"`
	Board   string `json:"board,omitempty"`
	Rev     int64  `json:"rev"`
	Machine string `json:"machine"`
	At      string `json:"at"`
	Reason  string `json:"reason"`
}

var boardPath = regexp.MustCompile(`^([^/]+)/project/([^/]+\.dc\.html)$`)

// BoardOf splits a path into project and board file when it is a board.
func BoardOf(rel string) (project, board string, ok bool) {
	m := boardPath.FindStringSubmatch(rel)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// SmallEdit: the two versions differ only in a short stretch, found by cutting the common start and end away.
func SmallEdit(a, b []byte) bool {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	j := 0
	for j < len(a)-i && j < len(b)-i && a[len(a)-1-j] == b[len(b)-1-j] {
		j++
	}
	return (len(a)-i-j)+(len(b)-i-j) <= smallEditBytes
}

var idUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// VariantID names the project a losing edit is saved as; at most 64 characters.
func VariantID(project, machine string, at time.Time) string {
	m := strings.Trim(idUnsafe.ReplaceAllString(strings.ToLower(machine), "-"), "-")
	if m == "" {
		m = "vm"
	}
	suffix := "-variant-" + m + "-" + at.UTC().Format("20060102-1504")
	if len(suffix) > 40 {
		suffix = suffix[:40]
	}
	head := project
	if len(head)+len(suffix) > 64 {
		head = head[:64-len(suffix)]
	}
	return head + suffix
}

// byRevision handles a write that lost: see the top of this file.
func (a *Agent) byRevision(ctx context.Context, rel string, data []byte, current filestore.Entry) error {
	at := a.now()
	n := Notice{V: 1, Kind: KindKept, Path: rel, CurrentRev: current.Rev, By: current.Author, ByMachine: current.Machine,
		Machine: a.cfg.Machine, At: at.UTC().Format(time.RFC3339), Bytes: len(data)}
	kept, err := a.claimKeptName(path.Join(KeptDir, rel+"."+a.cfg.Machine+"."+at.UTC().Format("20060102-150405")))
	if err != nil {
		return err
	}
	n.Kept = kept
	project, board, isBoard := BoardOf(rel)
	if isBoard {
		n.Project, n.Board, n.Kind = project, board, KindVariant
		if !current.Deleted {
			fresh, err := a.fetch(ctx, current)
			if err != nil {
				return err
			}
			if SmallEdit(data, fresh) {
				n.Kind = KindReapply
			}
		}
	}
	if err := a.place(n.Kept, bytes.NewReader(data), false); err != nil {
		return err
	}
	var variantFiles []string
	if n.Kind == KindVariant {
		id, err := a.claimVariantDir(VariantID(project, a.cfg.Machine, at))
		if err != nil {
			return err
		}
		n.Variant = id
		files, err := a.writeVariant(n, data)
		if err != nil {
			return err
		}
		variantFiles = files
	}
	raw, _ := json.MarshalIndent(n, "", "  ")
	if err := a.place(n.Kept+".json", bytes.NewReader(append(raw, '\n')), false); err != nil {
		return err
	}
	a.log.Warn("conflict: the write lost by revision, kept", "path", rel, "kind", n.Kind, "kept", n.Kept, "variant", n.Variant,
		"current", current.Rev, "by", current.Author+"@"+current.Machine)
	if err := a.apply(ctx, current); err != nil {
		return err
	}
	for _, f := range variantFiles {
		fi, err := a.root.Lstat(f)
		if err != nil {
			return err
		}
		if err := a.scanFile(ctx, f, fi); err != nil {
			return err
		}
	}
	return nil
}

// writeVariant makes the project of a variant: the original's artifact.json (title marked) and owner, the board with
// the losing contents, and the link record. It returns the files, relative to the root.
func (a *Agent) writeVariant(n Notice, data []byte) ([]string, error) {
	files := map[string][]byte{}
	var order []string
	add := func(name string, body []byte) {
		files[n.Variant+"/"+name] = body
		order = append(order, n.Variant+"/"+name)
	}
	meta := map[string]any{}
	if b, _, err := a.readRegular(n.Project + "/artifact.json"); err == nil {
		_ = json.Unmarshal(b, &meta)
	} else if !errors.Is(err, fs.ErrNotExist) {
		a.log.Warn("variant: artifact.json not read", "project", n.Project, "err", err)
	}
	title, _ := meta["title"].(string)
	if title == "" {
		title = n.Project
	}
	meta["title"] = title + " (вариант)"
	body, _ := json.MarshalIndent(meta, "", "  ")
	add("artifact.json", append(body, '\n'))
	if b, _, err := a.readRegular(n.Project + "/.owner.json"); err == nil {
		add(".owner.json", b)
	}
	add("project/"+n.Board, data)
	link, _ := json.MarshalIndent(VariantLink{V: 1, Of: n.Project, Board: n.Board, Rev: n.CurrentRev, Machine: n.Machine, At: n.At,
		Reason: "conflict"}, "", "  ")
	add(VariantFile, append(link, '\n'))
	for _, name := range order {
		if err := a.place(name, bytes.NewReader(files[name]), false); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// claimVariantDir creates the folder of a variant project and returns its name: the folder is made exclusively, and a
// name that is taken (a second conflict in the same second, or a project of that name) gets -2, -3, … — a variant is
// never written into a folder that exists.
func (a *Agent) claimVariantDir(id string) (string, error) {
	for i := 1; i < 1000; i++ {
		cand := id
		if i > 1 {
			suffix := "-" + strconv.Itoa(i)
			head := id
			if len(head)+len(suffix) > 64 {
				head = head[:64-len(suffix)]
			}
			cand = head + suffix
		}
		err := a.root.Mkdir(cand, 0o777)
		if err == nil {
			return cand, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("no free name for the variant project " + id)
}

// claimKeptName reserves the name the losing contents are kept under: the file is created exclusively (empty, place
// fills it), a taken name gets .2, .3, …, any other error is returned, and the attempts are capped.
func (a *Agent) claimKeptName(name string) (string, error) {
	if err := a.root.MkdirAll(path.Dir(name), 0o777); err != nil {
		return "", err
	}
	for i := 1; i < 1000; i++ {
		cand := name
		if i > 1 {
			cand = name + "." + strconv.Itoa(i)
		}
		f, err := a.root.OpenFile(cand, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			return cand, f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("no free name to keep " + name)
}
