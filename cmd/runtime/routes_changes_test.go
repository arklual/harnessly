package main

import (
	"testing"

	"harnessly/internal/checkpoints/scripttest"
	"harnessly/internal/gate"
)

func TestGoldenChangesRoute(t *testing.T) {
	replayGolden(t, "testdata/changes-golden.json", nil, func(*scripttest.World, goldenStep) {}, func(f *gate.Gate, d deps) {
		f.Add(f.AsRoutes(changesRoutes(d)...)...)
		f.Add(f.AsRoutes(gitRoutes(d)...)...)
	})
}
