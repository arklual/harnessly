package services

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The organizer: the person's projects, which project a conversation belongs to, and the ordered pins, in one small
// JSON file. A file that exists but cannot be trusted is never overwritten.

const (
	orgProjects    = 100
	orgPins        = 20
	orgAssignments = 5000
	orgNameLength  = 60
	orgOps         = 50
	orgFileBytes   = 256 * 1024
)

var (
	refRe       = regexp.MustCompile(`^(chat:[A-Za-z0-9-]{8,64}|agent:([a-z][a-z0-9-]{0,63}:)?[A-Za-z0-9_-]{1,64})$`)
	projectIDRe = regexp.MustCompile(`^prj-[A-Za-z0-9_-]{6,32}$`)
	colors      = map[string]bool{"blue": true, "green": true, "amber": true, "rose": true, "purple": true, "teal": true}
)

// ErrOrganizerUnreadable: the file exists but cannot be trusted; the route answers 500.
var ErrOrganizerUnreadable = errors.New("organizer-unreadable")

// OrganizerError is a refused change: its status and body.
type OrganizerError struct {
	Status int
	Body   *jsvalue.Object
}

func invalidOp(index int, reason string) *OrganizerError {
	return &OrganizerError{400, obj("error", "invalid-op", "index", index, "reason", reason)}
}

func isRef(v any) bool { s, ok := v.(string); return ok && refRe.MatchString(s) }

func isColor(v any) bool { s, ok := v.(string); return ok && colors[s] }

func isIndex(v any) bool {
	f, ok := v.(float64)
	return ok && f == math.Trunc(f) && !math.IsInf(f, 0) && f >= 0
}

func isInteger(v any) bool {
	f, ok := v.(float64)
	return ok && f == math.Trunc(f) && !math.IsInf(f, 0)
}

func normalizeProjectName(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	name := jscompat.Trim(spaceRunRe.ReplaceAllString(s, " "))
	return name, name != "" && jscompat.Len(name) <= orgNameLength
}

func refList(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok || len(list) == 0 || len(list) > orgAssignments {
		return nil, false
	}
	out := make([]string, len(list))
	for i, e := range list {
		if !isRef(e) {
			return nil, false
		}
		out[i] = e.(string)
	}
	return out, true
}

// organizerOp is one validated change.
type organizerOp struct {
	op        string
	id        string
	name      *string
	color     *string
	index     *float64
	ids       []string
	refs      []string
	ref       string
	projectID *string
}

func parseOrganizerOp(raw any, index int) (*organizerOp, *OrganizerError) {
	if !isRecord(raw) {
		return nil, invalidOp(index, "op must be an object")
	}
	fail := func(reason string) (*organizerOp, *OrganizerError) { return nil, invalidOp(index, reason) }
	id, idOK := opt(raw, "id").(string)
	idOK = idOK && projectIDRe.MatchString(id)
	indexOf := func() (*float64, bool) {
		v := opt(raw, "index")
		if isUndefined(v) {
			return nil, true
		}
		if !isIndex(v) {
			return nil, false
		}
		f := v.(float64)
		return &f, true
	}
	op, _ := opt(raw, "op").(string)
	switch op {
	case "project.create":
		name, nameOK := normalizeProjectName(opt(raw, "name"))
		if !idOK {
			return fail("bad project id")
		}
		if !nameOK {
			return fail("bad name")
		}
		if !isColor(opt(raw, "color")) {
			return fail("bad color")
		}
		at, ok := indexOf()
		if !ok {
			return fail("bad index")
		}
		color := opt(raw, "color").(string)
		return &organizerOp{op: op, id: id, name: &name, color: &color, index: at}, nil
	case "project.update":
		if !idOK {
			return fail("bad project id")
		}
		var name *string
		if v := opt(raw, "name"); !isUndefined(v) {
			n, ok := normalizeProjectName(v)
			if !ok {
				return fail("bad name")
			}
			name = &n
		}
		var color *string
		if v := opt(raw, "color"); !isUndefined(v) {
			if !isColor(v) {
				return fail("bad color")
			}
			c := v.(string)
			color = &c
		}
		if name == nil && color == nil {
			return fail("nothing to update")
		}
		return &organizerOp{op: op, id: id, name: name, color: color}, nil
	case "project.delete":
		if !idOK {
			return fail("bad project id")
		}
		return &organizerOp{op: op, id: id}, nil
	case "project.reorder":
		list, ok := opt(raw, "ids").([]any)
		if !ok {
			return fail("ids must be strings")
		}
		ids := make([]string, len(list))
		for i, e := range list {
			s, ok := e.(string)
			if !ok {
				return fail("ids must be strings")
			}
			ids[i] = s
		}
		return &organizerOp{op: op, ids: ids}, nil
	case "assign":
		refs, ok := refList(opt(raw, "refs"))
		if !ok {
			return fail("bad refs")
		}
		pid := opt(raw, "projectId")
		if pid == nil {
			return &organizerOp{op: op, refs: refs}, nil
		}
		p, ok := pid.(string)
		if !ok || !projectIDRe.MatchString(p) {
			return fail("bad project id")
		}
		return &organizerOp{op: op, refs: refs, projectID: &p}, nil
	case "pin":
		if !isRef(opt(raw, "ref")) {
			return fail("bad ref")
		}
		at, ok := indexOf()
		if !ok {
			return fail("bad index")
		}
		return &organizerOp{op: op, ref: opt(raw, "ref").(string), index: at}, nil
	case "unpin":
		if !isRef(opt(raw, "ref")) {
			return fail("bad ref")
		}
		return &organizerOp{op: op, ref: opt(raw, "ref").(string)}, nil
	case "pins.reorder":
		list, ok := opt(raw, "refs").([]any)
		if !ok {
			return fail("bad refs")
		}
		refs := make([]string, len(list))
		for i, e := range list {
			if !isRef(e) {
				return fail("bad refs")
			}
			refs[i] = e.(string)
		}
		return &organizerOp{op: op, refs: refs}, nil
	case "forget":
		refs, ok := refList(opt(raw, "refs"))
		if !ok {
			return fail("bad refs")
		}
		return &organizerOp{op: op, refs: refs}, nil
	}
	return fail("unknown op")
}

// ParseOrganizerOps validates a PATCH body: {ops: [1..50 ops]}.
func ParseOrganizerOps(body any) ([]*organizerOp, *OrganizerError) {
	var ops any = undefined
	if isRecord(body) {
		ops = opt(body, "ops")
	}
	list, ok := ops.([]any)
	if !ok || len(list) == 0 || len(list) > orgOps {
		return nil, invalidOp(-1, "ops must be an array of 1.."+strconv.Itoa(orgOps))
	}
	out := make([]*organizerOp, 0, len(list))
	for i, raw := range list {
		op, err := parseOrganizerOp(raw, i)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

func emptyOrganizer() *jsvalue.Object {
	return obj("version", 1, "revision", 0, "updatedAt", "1970-01-01T00:00:00.000Z", "projects", []any{}, "assignments", obj(), "pins", []any{})
}

// isOrganizer is the structural check of a file read from disk.
func isOrganizer(v any) bool {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return false
	}
	if ver, ok := opt(o, "version").(float64); !ok || ver != 1 {
		return false
	}
	if !isInteger(opt(o, "revision")) {
		return false
	}
	if _, ok := opt(o, "updatedAt").(string); !ok {
		return false
	}
	projects, ok1 := opt(o, "projects").([]any)
	pins, ok2 := opt(o, "pins").([]any)
	assignments, ok3 := opt(o, "assignments").(*jsvalue.Object)
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	for _, p := range projects {
		if !isRecord(p) {
			return false
		}
		_, idOK := opt(p, "id").(string)
		_, nameOK := opt(p, "name").(string)
		_, atOK := opt(p, "createdAt").(float64)
		if !idOK || !nameOK || !isColor(opt(p, "color")) || !atOK {
			return false
		}
	}
	for _, p := range pins {
		if !isRef(p) {
			return false
		}
	}
	for _, k := range assignments.Keys() {
		if _, ok := opt(assignments, k).(string); !ok {
			return false
		}
	}
	return true
}

func (s *Service) organizerFile() string { return s.cfg.OrganizerFile }

func (s *Service) readOrganizer() (*jsvalue.Object, error) {
	raw, err := os.ReadFile(s.organizerFile())
	if errors.Is(err, os.ErrNotExist) {
		return emptyOrganizer(), nil
	}
	if err != nil {
		return nil, ErrOrganizerUnreadable
	}
	text := files.DecodeUTF8(raw)
	if len(text) > orgFileBytes {
		return nil, ErrOrganizerUnreadable
	}
	v, err := jsvalue.Parse([]byte(text))
	if err != nil || !isOrganizer(v) {
		return nil, ErrOrganizerUnreadable
	}
	return v.(*jsvalue.Object), nil
}

func (s *Service) writeOrganizer(o *jsvalue.Object) error {
	content := jsvalue.Stringify(o, "  ")
	if len(content) > orgFileBytes {
		return ErrOrganizerUnreadable
	}
	file := s.organizerFile()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Organizer reads the file: an empty organizer when there is none yet.
func (s *Service) Organizer() (*jsvalue.Object, error) {
	s.organize.Lock()
	defer s.organize.Unlock()
	return s.readOrganizer()
}

// ApplyOrganizer applies every op, all or nothing, and writes the file only when something changed.
func (s *Service) ApplyOrganizer(ops []*organizerOp, nowMs float64) (*jsvalue.Object, *OrganizerError, error) {
	s.organize.Lock()
	defer s.organize.Unlock()
	current, err := s.readOrganizer()
	if err != nil {
		return nil, nil, err
	}
	next, changed, refusal := applyOrganizerOps(current, ops, nowMs)
	if refusal != nil {
		return nil, refusal, nil
	}
	if changed {
		if err := s.writeOrganizer(next); err != nil {
			return nil, nil, err
		}
	}
	return next, nil, nil
}

func cloneOrganizer(o *jsvalue.Object) *jsvalue.Object {
	next := o.Copy()
	projects := []any{}
	for _, p := range opt(o, "projects").([]any) {
		projects = append(projects, p.(*jsvalue.Object).Copy())
	}
	next.Set("projects", projects)
	next.Set("assignments", opt(o, "assignments").(*jsvalue.Object).Copy())
	next.Set("pins", append([]any{}, opt(o, "pins").([]any)...))
	return next
}

func insertAt(list []any, item any, index *float64) []any {
	at := len(list)
	if index != nil && *index < float64(len(list)) {
		at = int(*index)
	}
	list = append(list, nil)
	copy(list[at+1:], list[at:])
	list[at] = item
	return list
}

func isPermutation(next []string, current []any) bool {
	if len(next) != len(current) {
		return false
	}
	seen := map[string]bool{}
	for _, n := range next {
		if seen[n] {
			return false
		}
		seen[n] = true
		found := false
		for _, c := range current {
			if c == n {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func findProject(projects []any, id string) *jsvalue.Object {
	for _, p := range projects {
		if po := p.(*jsvalue.Object); opt(po, "id") == id {
			return po
		}
	}
	return nil
}

func withoutVolatile(o *jsvalue.Object) []byte {
	c := o.Copy()
	c.Set("revision", 0)
	c.Set("updatedAt", "")
	return jsvalue.Stringify(c, "")
}

func applyOrganizerOps(current *jsvalue.Object, ops []*organizerOp, now float64) (*jsvalue.Object, bool, *OrganizerError) {
	next := cloneOrganizer(current)
	limit := func(what string) *OrganizerError { return &OrganizerError{413, obj("error", "limit", "what", what)} }
	notFound := func(id string) *OrganizerError {
		return &OrganizerError{404, obj("error", "project-not-found", "id", id)}
	}
	for index, op := range ops {
		projects := opt(next, "projects").([]any)
		assignments := opt(next, "assignments").(*jsvalue.Object)
		pins := opt(next, "pins").([]any)
		var project *jsvalue.Object
		if op.op == "project.create" || op.op == "project.update" || op.op == "project.delete" {
			project = findProject(projects, op.id)
		}
		switch op.op {
		case "project.create":
			if project != nil {
				if opt(project, "name") != *op.name {
					return nil, false, &OrganizerError{409, obj("error", "project-exists", "id", op.id)}
				}
				continue
			}
			if len(projects) >= orgProjects {
				return nil, false, limit("projects")
			}
			next.Set("projects", insertAt(projects, obj("id", op.id, "name", *op.name, "color", *op.color, "createdAt", now), op.index))
		case "project.update":
			if project == nil {
				return nil, false, notFound(op.id)
			}
			if op.name != nil {
				project.Set("name", *op.name)
			}
			if op.color != nil {
				project.Set("color", *op.color)
			}
		case "project.delete":
			if project == nil {
				return nil, false, notFound(op.id)
			}
			kept := []any{}
			for _, p := range projects {
				if opt(p, "id") != op.id {
					kept = append(kept, p)
				}
			}
			next.Set("projects", kept)
			for _, k := range assignments.Keys() {
				if opt(assignments, k) == op.id {
					assignments.Delete(k)
				}
			}
		case "project.reorder":
			ids := make([]any, len(projects))
			for i, p := range projects {
				ids[i] = opt(p, "id")
			}
			if !isPermutation(op.ids, ids) {
				return nil, false, invalidOp(index, "ids must be a permutation of all projects")
			}
			ordered := make([]any, len(op.ids))
			for i, id := range op.ids {
				ordered[i] = findProject(projects, id)
			}
			next.Set("projects", ordered)
		case "assign":
			if op.projectID != nil && findProject(projects, *op.projectID) == nil {
				return nil, false, notFound(*op.projectID)
			}
			for _, ref := range op.refs {
				if op.projectID == nil {
					assignments.Delete(ref)
				} else {
					assignments.Set(ref, *op.projectID)
				}
			}
			if assignments.Len() > orgAssignments {
				return nil, false, limit("assignments")
			}
		case "pin":
			without := []any{}
			for _, p := range pins {
				if p != op.ref {
					without = append(without, p)
				}
			}
			if len(without) >= orgPins {
				return nil, false, limit("pins")
			}
			next.Set("pins", insertAt(without, op.ref, op.index))
		case "unpin":
			kept := []any{}
			for _, p := range pins {
				if p != op.ref {
					kept = append(kept, p)
				}
			}
			next.Set("pins", kept)
		case "pins.reorder":
			if !isPermutation(op.refs, pins) {
				return nil, false, invalidOp(index, "refs must be a permutation of all pins")
			}
			next.Set("pins", jsvalue.StringArray(op.refs))
		case "forget":
			drop := map[string]bool{}
			for _, r := range op.refs {
				drop[r] = true
			}
			kept := []any{}
			for _, p := range pins {
				if s, _ := p.(string); !drop[s] {
					kept = append(kept, p)
				}
			}
			next.Set("pins", kept)
			for _, r := range op.refs {
				assignments.Delete(r)
			}
		}
	}
	if string(withoutVolatile(next)) == string(withoutVolatile(current)) {
		return current, false, nil
	}
	rev, _ := opt(current, "revision").(float64)
	next.Set("revision", rev+1)
	iso, _ := jscompat.ISOString(now)
	next.Set("updatedAt", iso)
	return next, true, nil
}
