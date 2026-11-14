package designaccess

import (
	"context"
	"testing"
	"time"
)

// an access news goes to the bell and the mail both, with a key that names the request.
func TestSendRingsBellAndMails(t *testing.T) {
	type ring struct{ machine, to, key, title string }
	rung := make(chan ring, 1)
	mailed := make(chan string, 1)
	s := &Service{
		bell: func(_ context.Context, machineID, to, key, title, _, _ string) {
			rung <- ring{machineID, to, key, title}
		},
		notice: func(_ context.Context, to, _, _, _ string) error { mailed <- to; return nil },
	}
	s.send("m1", "a@example.com", "access-request:r1", "Ann asks", "please", "/l")
	select {
	case r := <-rung:
		if r != (ring{"m1", "a@example.com", "access-request:r1", "Ann asks"}) {
			t.Fatalf("%+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no bell")
	}
	select {
	case to := <-mailed:
		if to != "a@example.com" {
			t.Fatal(to)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no mail")
	}
}
