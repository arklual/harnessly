package fsprobe

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestStatReal(t *testing.T) {
	st, err := Stat(t.TempDir())
	if err != nil || !st.IsDir() {
		t.Fatalf("Stat(tempdir) = %v, %v", st, err)
	}
	if _, err := Stat("/definitely/not/here"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path: %v", err)
	}
}

func TestStatTimesOutOnHungCall(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	_, err := stat("/x", 50*time.Millisecond, func(string) (os.FileInfo, error) { <-block; return nil, nil })
	if !errors.Is(err, ErrUnavailable) || time.Since(start) > time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(start))
	}
}

func TestStuckProbesAreCapped(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	hang := func(string) (os.FileInfo, error) { <-block; return nil, nil }
	for i := 0; i < MaxStuck; i++ {
		if _, err := stat("/x", 5*time.Millisecond, hang); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("probe %d: %v", i, err)
		}
	}
	start := time.Now()
	if _, err := stat("/x", time.Second, hang); !errors.Is(err, ErrUnavailable) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("over the cap must fail at once: %v after %v", err, time.Since(start))
	}
}
