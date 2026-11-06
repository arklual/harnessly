package memstore

import (
	"context"
	"errors"
	"sort"
)

// Layered puts an external store over the files: what the adapter has wins, what it does not have (or cannot say
// because it is unreachable) comes from the files, so the agent never loses the memory it had.
type Layered struct {
	Top  Store
	Base Store
}

func fallsBack(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnreachable) || errors.Is(err, ErrUnsupported)
}

func (l Layered) List(ctx context.Context) ([]Entry, error) {
	base, _ := l.Base.List(ctx)
	top, err := l.Top.List(ctx)
	if err != nil && !fallsBack(err) {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Entry
	for _, e := range top {
		seen[e.Name] = true
		out = append(out, e)
	}
	for _, e := range base {
		if !seen[e.Name] {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (l Layered) Get(ctx context.Context, name string) (Note, error) {
	n, err := l.Top.Get(ctx, name)
	if err == nil || !fallsBack(err) {
		return n, err
	}
	return l.Base.Get(ctx, name)
}

// Put goes to the adapter; a store without write (or an unreachable one) falls to the files, which refuse when they
// have nowhere to write.
func (l Layered) Put(ctx context.Context, name, text, ifVersion string) (Entry, error) {
	e, err := l.Top.Put(ctx, name, text, ifVersion)
	if err == nil || !errors.Is(err, ErrUnsupported) {
		return e, err
	}
	return l.Base.Put(ctx, name, text, ifVersion)
}

func (l Layered) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 20
	}
	top, err := l.Top.Search(ctx, query, limit)
	if err != nil && !fallsBack(err) {
		return nil, err
	}
	base, _ := l.Base.Search(ctx, query, limit)
	seen := map[string]bool{}
	var out []Hit
	for _, h := range append(top, base...) {
		if seen[h.Name] || len(out) == limit {
			continue
		}
		seen[h.Name] = true
		out = append(out, h)
	}
	return out, nil
}
