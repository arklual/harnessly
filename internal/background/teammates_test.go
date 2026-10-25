package background

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func at(s string) Ms {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return Ms(t.UnixMilli())
}

// testdata/teammates.jsonl is a lead's transcript in the shapes Claude Code 2.1 writes: two named teammates spawned,
// idle notices, a message that wakes one, an idle notice delivered late, request copies of the calls, a tool that
// prints a teammate message, a peer that is no teammate, and a shutdown. Each prefix of it is a moment of the chat.
func TestTeammatesWorkWaitAndShutDown(t *testing.T) {
	raw, err := os.ReadFile("testdata/teammates.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	type mate struct {
		state string
		from  Ms
		since Ms
	}
	states := func(n int) map[string]mate {
		out := map[string]mate{}
		for _, m := range ParseTeammates(lines[:n], 1) {
			out[m.Name] = mate{m.State, m.WorkFrom, m.Since}
		}
		return out
	}
	spawnG1, spawnH1 := at("2026-10-25T02:42:20.708Z"), at("2026-10-25T02:42:46.500Z")
	fix, bye := at("2026-10-25T02:58:29.829Z"), at("2026-10-25T03:06:00.000Z")
	for _, c := range []struct {
		upTo int
		want map[string]mate
	}{
		{1, map[string]mate{}}, // the call alone starts nobody
		{2, map[string]mate{"impl-g1": {TeammateWorking, spawnG1, 0}}},
		{6, map[string]mate{"impl-g1": {TeammateWorking, spawnG1, 0}, "impl-h1": {TeammateWorking, spawnH1, 0}}},
		{7, map[string]mate{"impl-g1": {TeammateIdle, spawnG1, at("2026-10-25T02:55:58.120Z")}, "impl-h1": {TeammateWorking, spawnH1, 0}}},
		{8, map[string]mate{"impl-g1": {TeammateIdle, spawnG1, at("2026-10-25T02:55:58.120Z")}, "impl-h1": {TeammateWorking, spawnH1, 0}}},
		{9, map[string]mate{"impl-g1": {TeammateWorking, fix, 0}, "impl-h1": {TeammateWorking, spawnH1, 0}}},
		{11, map[string]mate{"impl-g1": {TeammateWorking, fix, 0}, "impl-h1": {TeammateWorking, spawnH1, 0}}},
		{13, map[string]mate{"impl-g1": {TeammateIdle, fix, at("2026-10-25T03:05:09.000Z")}, "impl-h1": {TeammateIdle, spawnH1, at("2026-10-25T03:00:59.000Z")}}},
		{14, map[string]mate{"impl-g1": {TeammateWorking, bye, 0}, "impl-h1": {TeammateWorking, bye, 0}}},
		{15, map[string]mate{"impl-g1": {TeammateShutDown, bye, at("2026-10-25T03:06:03.000Z")}, "impl-h1": {TeammateWorking, bye, 0}}},
	} {
		got := states(c.upTo)
		if len(got) != len(c.want) {
			t.Errorf("%d lines: %+v, want %+v", c.upTo, got, c.want)
			continue
		}
		for name, w := range c.want {
			if got[name] != w {
				t.Errorf("%d lines: %s is %+v, want %+v", c.upTo, name, got[name], w)
			}
		}
	}
	team := ParseTeammates(lines, 1)
	if len(team) != 2 || team[0].Name != "impl-h1" || team[1].ID != "aimpl-g1-28a3c1380862b8bb" ||
		team[1].Description != "G1: access tables + data layer" || team[1].Model != "sonnet" || team[1].SpawnedAt != spawnG1 {
		t.Fatalf("%+v", team)
	}
	// Teammates are no background tasks of the old kind: the subagent list and the footer stay as they were.
	if tasks := ParseTasks(lines, 1); len(tasks) != 0 {
		t.Errorf("%+v", tasks)
	}
}

// The reader keeps the teammates with its cursor: lines appended later move them on.
func TestReaderFollowsTeammates(t *testing.T) {
	raw, err := os.ReadFile("testdata/teammates.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(strings.TrimSpace(string(raw)), "\n")
	file := filepath.Join(t.TempDir(), "lead.jsonl")
	if err := os.WriteFile(file, []byte(strings.Join(lines[:7], "")), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Reader{Now: func() float64 { return 1 }}
	if team := r.Teammates(file); len(team) != 2 || team[1].State != TeammateIdle {
		t.Fatalf("%+v", team)
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Join(lines[7:9], "") + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if team := r.Teammates(file); team[1].State != TeammateWorking || team[1].WorkFrom != at("2026-10-25T02:58:29.829Z") {
		t.Fatalf("%+v", team)
	}
}
