package apps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Migrations of an app's document type: a declarative JSON Patch (RFC 6902: add, remove, replace, move, copy, test) per
// version, applied by the host, so a package needs no code to bring an old document up to date. A migration that fails
// leaves the document as it was; the caller rolls the install back (the host does not open a document it cannot bring to
// the version the editor knows).

// ErrMigration: a migration step could not be applied.
var ErrMigration = errors.New("apps: a migration failed")

// maxPatchOps bounds one step, and all the steps of one migration together.
const maxPatchOps = 10_000

// maxMigrationTime bounds the whole migration of one document.
const maxMigrationTime = 2 * time.Second

// patchBudget is what one migration may spend, over all its steps: the size the document may grow to (a copy or an add can
// double it, so the size is watched as it grows and not after), the operations, and the time.
type patchBudget struct {
	size, maxSize int
	ops, maxOps   int
	deadline      time.Time
}

func newPatchBudget(docBytes int) *patchBudget {
	return &patchBudget{size: docBytes, maxSize: scopeLimit[ScopeDoc], maxOps: maxPatchOps, deadline: time.Now().Add(maxMigrationTime)}
}

func (b *patchBudget) spend() error {
	if b.ops++; b.ops > b.maxOps {
		return fmt.Errorf("more than %d operations in all", b.maxOps)
	}
	if time.Now().After(b.deadline) {
		return errors.New("took too long")
	}
	return nil
}

func (b *patchBudget) grow(n int) error {
	if b.size += n; b.size > b.maxSize {
		return fmt.Errorf("the document would grow past %d bytes", b.maxSize)
	}
	return nil
}

func sizeOf(v any) int {
	raw, _ := json.Marshal(v)
	return len(raw)
}

// PatchOp is one operation of a JSON Patch.
type PatchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	From  string          `json:"from,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

// ParsePatch reads a JSON Patch and checks its shape (not its fit to any document).
func ParsePatch(raw []byte) ([]PatchOp, error) {
	var ops []PatchOp
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ops); err != nil {
		return nil, fmt.Errorf("a migration is a JSON Patch, an array of operations: %w", err)
	}
	if len(ops) > maxPatchOps {
		return nil, fmt.Errorf("at most %d operations in a step", maxPatchOps)
	}
	for i, op := range ops {
		switch op.Op {
		case "add", "replace", "test":
			if op.Value == nil {
				return nil, fmt.Errorf("operation %d (%s) needs a value", i, op.Op)
			}
		case "remove":
		case "move", "copy":
			if op.From == "" {
				return nil, fmt.Errorf("operation %d (%s) needs from", i, op.Op)
			}
			if _, err := pointer(op.From); err != nil {
				return nil, fmt.Errorf("operation %d (%s): from: %w", i, op.Op, err)
			}
		default:
			return nil, fmt.Errorf("operation %d: %q is not add, remove, replace, move, copy or test", i, op.Op)
		}
		if _, err := pointer(op.Path); err != nil {
			return nil, fmt.Errorf("operation %d (%s): path: %w", i, op.Op, err)
		}
	}
	return ops, nil
}

// pointer splits a JSON Pointer into its unescaped segments; "" is the whole document.
func pointer(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("%q is not a JSON Pointer (it starts with /)", p)
	}
	segs := strings.Split(p[1:], "/")
	for i, s := range segs {
		s = strings.ReplaceAll(s, "~1", "/")
		segs[i] = strings.ReplaceAll(s, "~0", "~")
	}
	return segs, nil
}

// ApplyPatch applies a step to a document. The input is not changed; an operation that does not fit fails the step.
func ApplyPatch(doc json.RawMessage, ops []PatchOp) (json.RawMessage, error) {
	return applyPatchWith(doc, ops, newPatchBudget(len(doc)))
}

// applyPatchWith applies a step under a budget that the steps of one migration share.
func applyPatchWith(doc json.RawMessage, ops []PatchOp, b *patchBudget) (json.RawMessage, error) {
	if len(doc) > b.maxSize {
		return nil, fmt.Errorf("%w: the document is larger than %d bytes", ErrMigration, b.maxSize)
	}
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, fmt.Errorf("%w: the document is not JSON: %v", ErrMigration, err)
	}
	for i, op := range ops {
		if err := b.spend(); err != nil {
			return nil, fmt.Errorf("%w: operation %d: %v", ErrMigration, i, err)
		}
		var err error
		root, err = applyOp(root, op, b)
		if err != nil {
			return nil, fmt.Errorf("%w: operation %d (%s %s): %v", ErrMigration, i, op.Op, op.Path, err)
		}
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	// What the next step starts from is what is really there, not the estimate.
	b.size = len(out)
	return out, nil
}

func applyOp(root any, op PatchOp, b *patchBudget) (any, error) {
	path, _ := pointer(op.Path)
	var value any
	if op.Value != nil {
		if err := json.Unmarshal(op.Value, &value); err != nil {
			return nil, err
		}
	}
	switch op.Op {
	case "add":
		if err := b.grow(sizeOf(value)); err != nil {
			return nil, err
		}
		return patchPut(root, path, value, true)
	case "replace":
		old, err := patchGet(root, path)
		if err != nil {
			return nil, err
		}
		if err := b.grow(sizeOf(value) - sizeOf(old)); err != nil {
			return nil, err
		}
		return patchPut(root, path, value, false)
	case "remove":
		old, err := patchGet(root, path)
		if err != nil {
			return nil, err
		}
		b.size -= sizeOf(old)
		return patchDel(root, path)
	case "test":
		got, err := patchGet(root, path)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(got, value) {
			return nil, errors.New("the test does not hold")
		}
		return root, nil
	case "move", "copy":
		from, _ := pointer(op.From)
		v, err := patchGet(root, from)
		if err != nil {
			return nil, err
		}
		// The size is checked before the copy is made: a copy of a big value is what doubles a document.
		if op.Op == "copy" {
			if err := b.grow(sizeOf(v)); err != nil {
				return nil, err
			}
		}
		v = patchClone(v)
		if op.Op == "move" {
			if root, err = patchDel(root, from); err != nil {
				return nil, err
			}
		}
		return patchPut(root, path, v, true)
	}
	return nil, errors.New("no such operation")
}

func patchClone(v any) any {
	raw, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(raw, &out)
	return out
}

func patchIndex(seg string, n int, allowEnd bool) (int, error) {
	if allowEnd && seg == "-" {
		return n, nil
	}
	i, err := strconv.Atoi(seg)
	if err != nil || i < 0 || strconv.Itoa(i) != seg || i > n || (i == n && !allowEnd) {
		return 0, fmt.Errorf("%q is not a place in the array", seg)
	}
	return i, nil
}

func patchGet(root any, path []string) (any, error) {
	cur := root
	for _, seg := range path {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[seg]
			if !ok {
				return nil, fmt.Errorf("there is no %q", seg)
			}
			cur = v
		case []any:
			i, err := patchIndex(seg, len(c), false)
			if err != nil {
				return nil, err
			}
			cur = c[i]
		default:
			return nil, fmt.Errorf("%q is in a value that has no members", seg)
		}
	}
	return cur, nil
}

// put sets (or, in an array with insert, inserts) the value at the path; the new root comes back.
func patchPut(root any, path []string, value any, insert bool) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	parent, err := patchGet(root, path[:len(path)-1])
	if err != nil {
		return nil, err
	}
	last := path[len(path)-1]
	switch p := parent.(type) {
	case map[string]any:
		p[last] = value
	case []any:
		i, err := patchIndex(last, len(p), insert)
		if err != nil {
			return nil, err
		}
		if insert {
			p = append(p, nil)
			copy(p[i+1:], p[i:])
			p[i] = value
		} else {
			p[i] = value
		}
		return reattach(root, path[:len(path)-1], p)
	default:
		return nil, errors.New("the place is in a value that has no members")
	}
	return root, nil
}

func patchDel(root any, path []string) (any, error) {
	if len(path) == 0 {
		return nil, errors.New("the whole document cannot be removed")
	}
	parent, err := patchGet(root, path[:len(path)-1])
	if err != nil {
		return nil, err
	}
	last := path[len(path)-1]
	switch p := parent.(type) {
	case map[string]any:
		if _, ok := p[last]; !ok {
			return nil, fmt.Errorf("there is no %q", last)
		}
		delete(p, last)
	case []any:
		i, err := patchIndex(last, len(p), false)
		if err != nil {
			return nil, err
		}
		return reattach(root, path[:len(path)-1], append(p[:i], p[i+1:]...))
	default:
		return nil, errors.New("the place is in a value that has no members")
	}
	return root, nil
}

// reattach puts a changed array back where it came from (a slice that grew or shrank is a new value).
func reattach(root any, path []string, arr []any) (any, error) {
	if len(path) == 0 {
		return arr, nil
	}
	parent, err := patchGet(root, path[:len(path)-1])
	if err != nil {
		return nil, err
	}
	last := path[len(path)-1]
	switch p := parent.(type) {
	case map[string]any:
		p[last] = arr
	case []any:
		i, err := patchIndex(last, len(p), false)
		if err != nil {
			return nil, err
		}
		p[i] = arr
	}
	return root, nil
}

// Migrate brings a document of the type from a version to the current one: each step in turn, all or nothing.
func (k SinopiaKind) Migrate(p *Package, doc json.RawMessage, from int) (json.RawMessage, error) {
	if from < 1 || from > k.SchemaVersion {
		return nil, fmt.Errorf("%w: a document of version %d, the format is at %d", ErrMigration, from, k.SchemaVersion)
	}
	cur := doc
	budget := newPatchBudget(len(doc))
	for v := from; v < k.SchemaVersion; v++ {
		file := k.Migrations[strconv.Itoa(v)]
		if file == "" {
			return nil, fmt.Errorf("%w: no step from version %d", ErrMigration, v)
		}
		raw, err := p.File(file)
		if err != nil {
			return nil, fmt.Errorf("%w: step %d: %v", ErrMigration, v, err)
		}
		ops, err := ParsePatch(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: step %d: %v", ErrMigration, v, err)
		}
		if cur, err = applyPatchWith(cur, ops, budget); err != nil {
			return nil, fmt.Errorf("step %d: %w", v, err)
		}
	}
	return cur, nil
}
