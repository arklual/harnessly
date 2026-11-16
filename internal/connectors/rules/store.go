package rules

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// Setting is the person's word on one template of one connection; nil fields are the template's defaults.
type Setting struct {
	On       *bool     `json:"on,omitempty"`
	Keywords *[]string `json:"keywords,omitempty"`
}

func (s Setting) on(t Template) bool {
	if s.On != nil {
		return *s.On
	}
	return t.On
}

func (s Setting) words(t Template) []string {
	if s.Keywords != nil {
		return slices.Clone(*s.Keywords)
	}
	return slices.Clone(t.Trigger.Keywords)
}

// Run is a run waiting to be started: a template, a connection and the event that asked, if any.
type Run struct {
	Key      string               `json:"key"`
	Template string               `json:"template"`
	Conn     string               `json:"conn"`
	At       time.Time            `json:"at"`
	Env      *connectors.Envelope `json:"env,omitempty"`
}

// Timer is a wait or a reminder that becomes a run at Fire.
type Timer struct {
	Key      string              `json:"key"`
	Template string              `json:"template"`
	Conn     string              `json:"conn"`
	Fire     time.Time           `json:"fire"`
	Env      connectors.Envelope `json:"env"`
}

// state is rules.json. It holds envelopes with their titles: it never leaves the machine, like the runner's outbox.
type state struct {
	Settings map[string]map[string]Setting `json:"settings,omitempty"`
	Pending  []Run                         `json:"pending,omitempty"`
	Timers   []Timer                       `json:"timers,omitempty"`
	// Slots are the days a daily template last ran: "<conn>|<template>" → YYYY-MM-DD.
	Slots map[string]string `json:"slots,omitempty"`
	// Counts are today's runs: "<conn>|<template>" (and "" for all) → n; Day names the day.
	Day    string         `json:"day,omitempty"`
	Counts map[string]int `json:"counts,omitempty"`
	// Done are the keys of runs already queued (the newest 2000): an event delivered twice runs once.
	Done []string `json:"done,omitempty"`
}

func (st *state) setting(conn, template string) Setting {
	return st.Settings[conn][template]
}

func (st *state) put(conn, template string, s Setting) {
	if st.Settings == nil {
		st.Settings = map[string]map[string]Setting{}
	}
	if st.Settings[conn] == nil {
		st.Settings[conn] = map[string]Setting{}
	}
	st.Settings[conn][template] = s
}

func (st *state) queue(r Run) {
	if slices.Contains(st.Done, r.Key) {
		return
	}
	st.Done = append(st.Done, r.Key)
	if len(st.Done) > 2000 {
		st.Done = st.Done[len(st.Done)-2000:]
	}
	st.Pending = append(st.Pending, r)
}

// dropOf removes the waiting runs and timers of a connection (and of one template, when named).
func (st *state) dropOf(conn, template string) {
	of := func(c, t string) bool { return c == conn && (template == "" || t == template) }
	st.Pending = slices.DeleteFunc(st.Pending, func(r Run) bool { return of(r.Conn, r.Template) })
	st.Timers = slices.DeleteFunc(st.Timers, func(x Timer) bool { return of(x.Conn, x.Template) })
	for k := range st.Slots {
		if strings.HasPrefix(k, conn+"|") && (template == "" || k == conn+"|"+template) {
			delete(st.Slots, k)
		}
	}
}

func day(t time.Time) string { return t.Format("2006-01-02") }

func (st *state) slot(conn, template string, now time.Time) {
	if st.Slots == nil {
		st.Slots = map[string]string{}
	}
	st.Slots[conn+"|"+template] = day(now)
}

func (st *state) slotDone(conn, template string, now time.Time) bool {
	return st.Slots[conn+"|"+template] == day(now)
}

func (st *state) count(now time.Time, key string) int {
	if st.Day != day(now) {
		return 0
	}
	return st.Counts[key]
}

func (st *state) add(now time.Time, key string) {
	if st.Day != day(now) {
		st.Day, st.Counts = day(now), map[string]int{}
	}
	st.Counts[key]++
	st.Counts[""]++
}

type store struct{ dir string }

func (s *store) file() string { return filepath.Join(s.dir, "rules.json") }

func (s *store) load() (*state, error) {
	st := &state{}
	raw, err := os.ReadFile(s.file())
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *store) save(st *state) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp := s.file() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file())
}
