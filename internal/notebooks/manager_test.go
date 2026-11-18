package notebooks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeKernel struct {
	closed      atomic.Bool
	count       atomic.Int32
	started     chan struct{}
	interrupted chan struct{}
	once        sync.Once
}

func (f *fakeKernel) Execute(ctx context.Context, code string) (Result, error) {
	n := int(f.count.Add(1))
	if code == "block" {
		close(f.started)
		select {
		case <-ctx.Done():
			return Result{}, contextError(ctx)
		case <-f.interrupted:
		}
	}
	return Result{Outputs: []json.RawMessage{json.RawMessage(`{"output_type":"stream","name":"stdout","text":"ok"}`)}, ExecutionCount: &n, Status: "ok"}, nil
}
func (f *fakeKernel) Interrupt(context.Context) error {
	f.once.Do(func() { close(f.interrupted) })
	return nil
}
func (f *fakeKernel) Close()      { f.closed.Store(true) }
func (f *fakeKernel) Alive() bool { return !f.closed.Load() }

func testManager(t *testing.T) (*Manager, *[]*fakeKernel) {
	t.Helper()
	m := New()
	var kernels []*fakeKernel
	m.start = func(context.Context, string) (kernel, error) {
		f := &fakeKernel{started: make(chan struct{}), interrupted: make(chan struct{})}
		kernels = append(kernels, f)
		return f, nil
	}
	t.Cleanup(m.Close)
	return m, &kernels
}

func TestNotebookStatusIsLazyAndCellsPersist(t *testing.T) {
	m, kernels := testManager(t)
	key := Key{"owner", "tab-a", "/project/book.ipynb"}
	if got := m.Status(key); got.State != "stopped" || len(*kernels) != 0 {
		t.Fatalf("GET started a kernel: %+v", got)
	}
	m.Shutdown(key)
	if _, err := m.Interrupt(context.Background(), key); err != nil || len(*kernels) != 0 {
		t.Fatalf("stopped interrupt: %v", err)
	}
	for i := 1; i <= 2; i++ {
		out, err := m.Execute(context.Background(), key, "x")
		if err != nil || out.ExecutionCount == nil || *out.ExecutionCount != i || out.Status != "ok" {
			t.Fatalf("cell %d: %+v %v", i, out, err)
		}
	}
	if len(*kernels) != 1 || m.Status(key).State != "idle" {
		t.Fatal("kernel was not persistent")
	}
	other := key
	other.Owner = "other"
	if m.Status(other).State != "stopped" {
		t.Fatal("kernel shared between owners")
	}
	other = key
	other.Conversation = "tab-b"
	if m.Status(other).State != "stopped" {
		t.Fatal("kernel shared between conversations")
	}
	other = key
	other.Path = "/project/other.ipynb"
	if m.Status(other).State != "stopped" {
		t.Fatal("kernel shared between paths")
	}
	if out, err := m.Restart(context.Background(), key); err != nil || out.State != "idle" || len(*kernels) != 2 || !(*kernels)[0].closed.Load() {
		t.Fatalf("restart: %+v %v", out, err)
	}
	out, err := m.Execute(context.Background(), key, "x")
	if err != nil || *out.ExecutionCount != 1 {
		t.Fatalf("restart retained state: %+v %v", out, err)
	}
	m.Shutdown(key)
	if !(*kernels)[1].closed.Load() || m.Status(key).State != "stopped" {
		t.Fatal("shutdown left a kernel")
	}
}

func TestNotebookBusyRejectsButInterruptAndShutdownWork(t *testing.T) {
	m, _ := testManager(t)
	key := Key{"owner", "tab-a", "/project/book.ipynb"}
	if _, err := m.Restart(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	f := m.sessions[key].kernel.(*fakeKernel)
	m.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := m.Execute(context.Background(), key, "block"); done <- err }()
	<-f.started
	if m.Status(key).State != "busy" {
		t.Fatal("busy state not visible")
	}
	for _, action := range []string{"execute", "restart"} {
		var err error
		if action == "execute" {
			_, err = m.Execute(context.Background(), key, "x")
		} else {
			_, err = m.Restart(context.Background(), key)
		}
		var ne *Error
		if !errors.As(err, &ne) || ne.Code != "busy" {
			t.Fatalf("%s not rejected: %v", action, err)
		}
	}
	if _, err := m.Interrupt(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt waited for execute")
	}
	// A second blocking execution is stopped by shutdown, not a serialized queue.
	f.started = make(chan struct{})
	f.interrupted = make(chan struct{})
	f.once = sync.Once{}
	go func() { _, err := m.Execute(context.Background(), key, "block"); done <- err }()
	<-f.started
	m.Shutdown(key)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown left execution running")
	}
}

func TestNotebookLimitsExpiryAndClose(t *testing.T) {
	m, kernels := testManager(t)
	ctx := context.Background()
	for i := 0; i < MaxOwnerSessions; i++ {
		key := Key{"owner", string(rune('a' + i)), "/project/book.ipynb"}
		if _, err := m.Execute(ctx, key, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Execute(ctx, Key{"owner", "overflow", "/project/book.ipynb"}, "x"); err == nil {
		t.Fatal("owner limit not enforced")
	}
	if _, err := m.Execute(ctx, Key{"another", "tab-a", "/project/book.ipynb"}, strings.Repeat("x", MaxCodeBytes+1)); err == nil {
		t.Fatal("code limit not enforced")
	}
	if len(*kernels) != MaxOwnerSessions {
		t.Fatal("limit started a kernel")
	}
	m.mu.Lock()
	for _, s := range m.sessions {
		s.used = time.Now().Add(-IdleTimeout - time.Second)
	}
	m.mu.Unlock()
	m.expire()
	for _, f := range *kernels {
		if f.Alive() {
			t.Fatal("expiry left a kernel")
		}
	}
	key := Key{"owner", "tab-a", "/project/book.ipynb"}
	if _, err := m.Execute(ctx, key, "x"); err != nil {
		t.Fatal(err)
	}
	m.CloseConversation("tab-a")
	if m.Status(key).State != "stopped" || (*kernels)[len(*kernels)-1].Alive() {
		t.Fatal("conversation close left a kernel")
	}
	if _, err := m.Restart(ctx, key); err != nil {
		t.Fatal(err)
	}
	m.Close()
	if (*kernels)[len(*kernels)-1].Alive() {
		t.Fatal("Close left a kernel")
	}
	if _, err := m.Execute(ctx, key, "x"); err == nil {
		t.Fatal("closed manager accepted execution")
	}
}

func TestNotebookCloseCancelsKernelStartup(t *testing.T) {
	m := New()
	started := make(chan struct{})
	m.start = func(ctx context.Context, _ string) (kernel, error) {
		close(started)
		<-ctx.Done()
		return nil, contextError(ctx)
	}
	finished := make(chan struct{})
	go func() {
		_, _ = m.Execute(context.Background(), Key{"owner", "tab-a", "/project/book.ipynb"}, "x")
		close(finished)
	}()
	<-started
	m.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Close did not wait for startup")
	}
}

func TestNotebookInterruptCancelsStartup(t *testing.T) {
	m := New()
	defer m.Close()
	started := make(chan struct{})
	m.start = func(ctx context.Context, _ string) (kernel, error) {
		close(started)
		<-ctx.Done()
		return nil, contextError(ctx)
	}
	key := Key{"owner", "tab-a", "/project/book.ipynb"}
	finished := make(chan error, 1)
	go func() { _, err := m.Execute(context.Background(), key, "x"); finished <- err }()
	<-started
	if out, err := m.Interrupt(context.Background(), key); err != nil || out.State != "stopped" {
		t.Fatalf("interrupt during startup: %+v %v", out, err)
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("startup executed after interrupt")
		}
	case <-time.After(time.Second):
		t.Fatal("interrupt did not cancel startup")
	}
}

func TestNotebookCleanEnvironment(t *testing.T) {
	t.Setenv("RUNTIME_TEST_TOKEN", "do-not-inherit")
	t.Setenv("CONNECTOR_TOKEN", "do-not-inherit")
	t.Setenv("PYTHONPATH", "/untrusted")
	t.Setenv("HTTP_PROXY", "http://credentials@example.org")
	vars := cleanEnv("/private/runtime")
	for _, entry := range vars {
		name, value, _ := strings.Cut(entry, "=")
		if name == "PYTHONPATH" || name == "HTTP_PROXY" || strings.HasPrefix(name, "RUNTIME_") || strings.HasPrefix(name, "CONNECTOR_") || strings.Contains(value, "do-not-inherit") {
			t.Fatalf("environment inherited %s", name)
		}
	}
	env := strings.Join(vars, "\n")
	if !strings.Contains(env, "IPYTHONDIR=/private/runtime") {
		t.Fatal("IPython config not isolated")
	}
}
