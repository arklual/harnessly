package main

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/sidewidget"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// a widget an agent makes, changes or removes reaches every open page by the machine channel (a machine_status with appsAt),
// with no reload, and a burst of writes is told once.
func TestAnAgentsWidgetIsToldToTheOpenPages(t *testing.T) {
	data := t.TempDir()
	m := newLiveMachine("m", "v", nil, t.TempDir(), nil, nil, nil)
	var mu sync.Mutex
	got := 0
	m.subs[1] = func(e wire.Event) {
		if st, ok := e.(wire.MachineStatusEv); ok && st.AppsAt != 0 {
			mu.Lock()
			got++
			mu.Unlock()
		}
	}
	reg := apps.Open(data)
	reg.OnChange(m.appsChanged)
	s := &sidewidget.Service{Reg: reg, Root: filepath.Join(data, "side-widgets"), Host: "0.16.999"}
	heard := func(want int, what string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := got
			mu.Unlock()
			if n >= want {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("%s: the pages were told %d times, want %d", what, got, want)
	}

	if _, err := s.Create(sidewidget.Spec{Name: "render-progress", Title: "Рендер", HTML: "<b>0%</b>"}); err != nil {
		t.Fatal(err)
	}
	heard(1, "create")
	html := "<b>50%</b>"
	if _, err := s.Update("render-progress", sidewidget.Patch{HTML: &html}); err != nil {
		t.Fatal(err)
	}
	heard(2, "update")
	if err := s.Remove("render-progress"); err != nil {
		t.Fatal(err)
	}
	heard(3, "remove")
}
