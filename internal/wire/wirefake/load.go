// Package wirefake is a runtime without agents: a convo.Service and a wire.MachineSource that serve the shared
// fixtures of internal/wire/testdata and play a scripted agent on them. A standalone runtime started with
// RUNTIME_V2_FAKE=1 serves it on /api/v2, so the page's end-to-end tests run on the real protocol, socket and
// commands with no Claude or Codex behind them; POST /api/v2/fake/<action> steers it (control.go).
package wirefake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// fixtureNow is the serverTime of the fixtures: their times are moved by now − fixtureNow, so a wait that was 3 s
// ahead of the fixture is 3 s ahead of the fake's start.
const fixtureNow int64 = 1792656000000

// fixture reads one fixture of wire.Fixtures, its times moved by shift and the fixture machine renamed to machine.
func fixture(name string, shift int64, machine string) ([]byte, error) {
	raw, err := fs.ReadFile(wire.Fixtures, "testdata/"+name+".json")
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	v = rewrite(v, "", shift, machine)
	return json.Marshal(v)
}

// rewrite moves every time of v (since, at and the *At fields) by shift and renames the fixture machine.
func rewrite(v any, key string, shift int64, machine string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = rewrite(e, k, shift, machine)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = rewrite(e, key, shift, machine)
		}
		return x
	case json.Number:
		if key == "since" || key == "at" || key == "serverTime" || strings.HasSuffix(key, "At") {
			if n, err := x.Int64(); err == nil && n > 1e12 {
				return json.Number(fmt.Sprint(n + shift))
			}
		}
		return x
	case string:
		if (key == "machineId" || key == "id") && x == "runner-2" {
			return machine
		}
		return x
	}
	return v
}

// snapshotOf reads a snapshot fixture through the strict decoder.
func snapshotOf(name string, shift int64, machine string) (wire.SnapshotFrame, error) {
	raw, err := fixture(name, shift, machine)
	if err != nil {
		return wire.SnapshotFrame{}, err
	}
	f, err := wire.DecodeServerFrame(raw)
	if err != nil {
		return wire.SnapshotFrame{}, fmt.Errorf("%s: %w", name, err)
	}
	snap, ok := f.(wire.SnapshotFrame)
	if !ok {
		return wire.SnapshotFrame{}, fmt.Errorf("%s: not a snapshot", name)
	}
	return snap, nil
}

// eventOf reads the event of an ev fixture.
func eventOf(name string, shift int64, machine string) (wire.Event, error) {
	raw, err := fixture(name, shift, machine)
	if err != nil {
		return nil, err
	}
	f, err := wire.DecodeServerFrame(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	ev, ok := f.(wire.EvFrame)
	if !ok {
		return nil, fmt.Errorf("%s: not an event", name)
	}
	return ev.E, nil
}

// clone deep-copies a value of the wire types through JSON.
func clone[T any](v T) T {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}
