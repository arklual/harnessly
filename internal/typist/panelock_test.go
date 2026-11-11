package typist

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func shortTerms(t *testing.T, hold, wait time.Duration) {
	t.Helper()
	h, w := holdLimit, waitLimit
	holdLimit, waitLimit = hold, wait
	t.Cleanup(func() { holdLimit, waitLimit = h, w })
}

// A writer that never lets the pane go loses it at the end of its term: the next writer gets in.
func TestAHungWriterLosesThePaneAtItsTerm(t *testing.T) {
	shortTerms(t, 60*time.Millisecond, 5*time.Second)
	told := make(chan string, 1)
	OnExpired = func(pane string, _ time.Duration) { told <- pane }
	t.Cleanup(func() { OnExpired = nil })
	before := Expired()

	hung := Lock("term-test-hung") // never unlocked by its writer
	start := time.Now()
	unlock, err := LockContext(context.Background(), "term-test-hung")
	if err != nil {
		t.Fatalf("the pane stayed with the hung writer: %v", err)
	}
	if waited := time.Since(start); waited < 30*time.Millisecond || waited > 2*time.Second {
		t.Errorf("the pane came after %s", waited)
	}
	if Expired() != before+1 {
		t.Errorf("expired holds: %d", Expired()-before)
	}
	select {
	case pane := <-told:
		if pane != "term-test-hung" {
			t.Errorf("told of %q", pane)
		}
	case <-time.After(time.Second):
		t.Error("nobody was told of the hold that ended by its term")
	}
	// The hung writer's own unlock, when it comes at last, does not take the pane from the one that holds it now.
	hung()
	if _, ok := TryLock("term-test-hung"); ok {
		t.Fatal("the late unlock of the hung writer freed the next writer's hold")
	}
	unlock()
	unlock() // safe twice
	if u, ok := TryLock("term-test-hung"); !ok {
		t.Fatal("the pane is free and was not taken")
	} else {
		u()
	}
}

// A hold let go in time is not let go again by its term.
func TestAHoldLetGoInTimeHasNoTerm(t *testing.T) {
	shortTerms(t, 40*time.Millisecond, time.Second)
	before := Expired()
	Lock("term-test-quick")()
	time.Sleep(120 * time.Millisecond)
	if Expired() != before {
		t.Errorf("expired holds: %d", Expired()-before)
	}
}

// A writer that waits for a pane is told it is busy — by its context or by the wait's own term — and holds nothing.
func TestAWaiterIsToldThePaneIsBusy(t *testing.T) {
	shortTerms(t, time.Minute, 80*time.Millisecond)
	held := Lock("term-test-busy")
	defer held()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := LockContext(ctx, "term-test-busy")
	var busy *BusyError
	if !errors.As(err, &busy) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a waiter whose context ended got %v", err)
	}
	if !strings.Contains(err.Error(), "term-test-busy") || !strings.Contains(err.Error(), "another writer") {
		t.Errorf("the error does not say what is in the way: %v", err)
	}

	start := time.Now()
	_, err = LockContext(context.Background(), "term-test-busy")
	if !errors.As(err, &busy) || busy.Cause != nil {
		t.Fatalf("a waiter past the wait's term got %v", err)
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("the waiter hung for %s", waited)
	}
	wrote := false
	if err := Write(context.Background(), nil, "term-test-busy", func(context.Context) error { wrote = true; return nil }); !errors.As(err, &busy) || wrote {
		t.Fatalf("Write into a held pane: err %v, wrote %v", err, wrote)
	}
}

type slowServer struct{ err error }

func (s slowServer) Responsive(string) error { return s.err }

// A terminal server that answers too slowly gets no keys: the write is put off, and the pane is free again.
func TestNothingIsWrittenIntoASlowTerminal(t *testing.T) {
	slow := errors.New("tmux display-message did not answer in 2.4s")
	wrote := 0
	err := Write(context.Background(), slowServer{slow}, "term-test-slow", func(context.Context) error { wrote++; return nil })
	if !IsSlow(err) || !errors.Is(err, slow) || wrote != 0 {
		t.Fatalf("err %v, wrote %d", err, wrote)
	}
	if err := Write(context.Background(), slowServer{}, "term-test-slow", func(context.Context) error { wrote++; return nil }); err != nil || wrote != 1 {
		t.Fatalf("a terminal that answers in time: err %v, wrote %d", err, wrote)
	}
	// Under the lock: the write holds the pane while it runs.
	_ = Write(context.Background(), nil, "term-test-slow", func(context.Context) error {
		if _, ok := TryLock("term-test-slow"); ok {
			t.Error("the write ran without the pane's lock")
		}
		return nil
	})
}

type slowPane struct {
	*fakePane
	err error
}

func (p slowPane) Responsive() error { return p.err }

// Type asks the pane before its first key: a slow terminal ends the try with nothing typed, to be tried later.
func TestASlowTerminalIsNotTypedInto(t *testing.T) {
	p := newPane(tui(t, "claude-idle-placeholder.ansi.txt"))
	res := Type(Message{Text: "hello"}, claude, slowPane{p, errors.New("slow")}, newClock())
	if res.Outcome != Wait || !res.Slow || !IsSlow(res.Err) || res.Typed || len(p.sent()) != 0 {
		t.Fatalf("outcome %s slow %v err %v typed %v keys %v", res.Outcome, res.Slow, res.Err, res.Typed, p.sent())
	}
	leaves(t, p)
	if res := Type(Message{Text: "hello"}, claude, slowPane{p, nil}, newClock()); res.Outcome != Sent || res.Slow {
		t.Fatalf("a terminal that answers in time: %s", res.Outcome)
	}
}

// paneKeys are the tmux calls that put keys into a pane.
var paneKeys = map[string]bool{
	"SendKeys": true, "SendKeysSeparated": true, "SendLiteral": true, "SendLiteralThenEnter": true, "SendRawKeys": true,
	"SendKeySequence": true, "SendBracketedPasteOnly": true, "SendBracketedPaste": true,
}

// callerHolds are the functions that put keys into a pane without taking its lock themselves, and why that is right:
// each is reached only from a caller that holds the lock, or types into a session no agent and no other writer has.
// A new name here needs the same proof; anything else goes through Write.
var callerHolds = map[string]string{
	"cmd/runtime/v2_machine.go":                    "v2Pane is the typist's Pane: the conversation types through it under typist.Lock",
	"cmd/runtime/selftest.go":                      "the self-test's own tmux session, made and killed by the test",
	"cmd/runtime/selftest_agent.go":                "the self-test's own tmux session, made and killed by the test",
	"internal/prompts/drivers.go":                  "the drivers of a submit: Service holds the pane (hold) around every one of them",
	"internal/prompts/editor.go":                   "the editor submit runs inside a submit that holds the pane",
	"internal/prompts/sendnow.go":                  "send-now runs inside a submit that holds the pane",
	"internal/prompts/routes.go:Key":               "keyIO answers a question dialog inside AnswerQuestion's hold",
	"internal/prompts/routes.go:Text":              "keyIO answers a question dialog inside AnswerQuestion's hold",
	"internal/prompts/routes.go:chatAboutQuestion": "called by AnswerQuestion and by a submit's driver, both inside a hold",
}

// Every key that reaches a pane is typed by a writer that holds the pane's lock: a function that calls one of tmux's
// key senders takes the lock in its own body (Write, WriteNow, Hold, Preempt, TypeHeld, Lock, or the prompts' hold), or is named
// in callerHolds with the reason its caller does.
func TestNoKeysReachAPaneOutsideItsLock(t *testing.T) {
	root := filepath.Join("..", "..")
	var loose []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
			if d.IsDir() {
				if rel == "internal/tmux" || rel == "internal/typist" || d.Name() == "testdata" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				keys, locked := false, false
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch f := call.Fun.(type) {
					case *ast.Ident:
						locked = locked || f.Name == "hold"
					case *ast.SelectorExpr:
						keys = keys || paneKeys[f.Sel.Name]
						if x, ok := f.X.(*ast.Ident); ok && x.Name == "typist" {
							switch f.Sel.Name {
							case "Write", "WriteNow", "Hold", "Preempt", "TypeHeld", "Lock", "LockContext", "TryLock":
								locked = true
							}
						}
					}
					return true
				})
				if !keys || locked {
					continue
				}
				if callerHolds[rel] != "" || callerHolds[rel+":"+fn.Name.Name] != "" {
					continue
				}
				loose = append(loose, rel+":"+fn.Name.Name)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(loose)
	if len(loose) > 0 {
		t.Fatalf("keys are typed into a pane without its lock (use typist.Write):\n  %s", strings.Join(loose, "\n  "))
	}
}
