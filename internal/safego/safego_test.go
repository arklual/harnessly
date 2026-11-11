package safego

import (
	"strings"
	"sync"
	"testing"
)

func TestTryHoldsAPanicWithItsStack(t *testing.T) {
	var name string
	var got Panic
	SetLog(func(n string, p Panic) { name, got = n, p })
	defer SetLog(nil)
	before := Panics()

	if p := Try("clean", func() {}); p != nil {
		t.Fatalf("a clean call reported a panic: %v", p.Value)
	}
	p := Try("chat c1", func() { panic("odd line") })
	if p == nil || p.Value != "odd line" {
		t.Fatalf("panic not returned: %+v", p)
	}
	if name != "chat c1" || got.Value != "odd line" || !strings.Contains(string(got.Stack), "safego_test.go") {
		t.Fatalf("log got name=%q value=%v stack=%q", name, got.Value, got.Stack)
	}
	if Panics() != before+1 {
		t.Fatalf("panics = %d, want %d", Panics(), before+1)
	}
}

func TestGoKeepsTheProcessAndRunsDefers(t *testing.T) {
	SetLog(func(string, Panic) {})
	defer SetLog(nil)
	var wg sync.WaitGroup
	wg.Add(1)
	Go("worker", func() {
		defer wg.Done()
		panic("odd input")
	})
	wg.Wait()
}

func TestAPanickingReporterIsHeldToo(t *testing.T) {
	SetLog(func(string, Panic) { panic("reporter") })
	defer SetLog(nil)
	if Run("x", func() { panic("first") }) {
		t.Fatal("Run said the call returned")
	}
}

func TestRunReturnsAfterAPanic(t *testing.T) {
	SetLog(func(string, Panic) {})
	defer SetLog(nil)
	ran := false
	Run("test", func() {
		ran = true
		panic("odd input")
	})
	if !ran {
		t.Fatal("fn did not run")
	}
}
