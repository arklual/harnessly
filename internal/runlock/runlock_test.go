package runlock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLock(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sub", "x.lock")
	at := time.UnixMilli(1792400400000)
	l, err := Acquire(Config{File: file, App: "acme", Now: func() time.Time { return at }}, 8022)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	want := "{\n  \"pid\": " + strconv.Itoa(os.Getpid()) + ",\n  \"port\": 8022,\n  \"startedAt\": 1792400400000\n}\n"
	if string(raw) != want {
		t.Fatalf("%q", raw)
	}
	if again, err := Acquire(Config{File: file, App: "acme"}, 9000); err != nil || again == nil {
		t.Fatal("this process holds it already")
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	// A live runtime on its port keeps it.
	holder := `{"pid":` + strconv.Itoa(os.Getppid()) + `,"port":4321,"startedAt":1}`
	_ = os.WriteFile(file, []byte(holder), 0o600)
	_, err = Acquire(Config{File: file, App: "acme", Health: func(port int) (string, bool) { return "acme", port == 4321 }}, 8022)
	var held *HeldError
	if !errors.As(err, &held) || held.Port != 4321 {
		t.Fatalf("%v", err)
	}
	l.Release() // not ours any more: left alone
	if raw, _ := os.ReadFile(file); string(raw) != holder {
		t.Fatal("released another runtime's lock")
	}
}
