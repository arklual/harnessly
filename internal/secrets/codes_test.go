package secrets

import (
	"context"
	"testing"
	"time"
)

func testCodes() (*Codes, *time.Time) {
	now := time.UnixMilli(1_800_000_000_000)
	c := NewCodes()
	c.Now = func() time.Time { return now }
	return c, &now
}

func TestCodeIsHandedOnceToItsChat(t *testing.T) {
	c, _ := testCodes()
	a, err := c.Open("npm publish vizloom", "conv-a", "Release", 6, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Pending(); len(got) != 1 || got[0].ID != a.ID || got[0].Digits != 6 || got[0].Why != "npm publish vizloom" {
		t.Fatalf("pending = %+v", got)
	}
	if _, err := c.Put(a.ID, "12345"); ReasonOf(err) != ReasonBadCode {
		t.Fatalf("five digits: %v", err)
	}
	if _, err := c.Put(a.ID, "12a456"); ReasonOf(err) != ReasonBadCode {
		t.Fatalf("a letter: %v", err)
	}
	if ok, err := c.Put(a.ID, "123456"); !ok || err != nil {
		t.Fatalf("put: %v %v", ok, err)
	}
	if len(c.Pending()) != 0 {
		t.Fatal("an answered ask is still pending")
	}
	if state, value := c.Wait(context.Background(), a.ID, "conv-b", time.Millisecond); state != CodeGone || value != "" {
		t.Fatalf("another chat got %q %q", state, value)
	}
	if state, value := c.Wait(context.Background(), a.ID, "conv-a", time.Millisecond); state != CodeReady || value != "123456" {
		t.Fatalf("wait = %q %q", state, value)
	}
	if state, value := c.Wait(context.Background(), a.ID, "conv-a", time.Millisecond); state != CodeGone || value != "" {
		t.Fatalf("second take = %q %q", state, value)
	}
}

func TestCodeWaiterWakesOnAnswer(t *testing.T) {
	c, _ := testCodes()
	a, _ := c.Open("x", "conv-a", "", 6, time.Minute)
	got := make(chan string, 1)
	go func() {
		_, v := c.Wait(context.Background(), a.ID, "conv-a", 10*time.Second)
		got <- v
	}()
	time.Sleep(20 * time.Millisecond)
	if _, err := c.Put(a.ID, "654321"); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v != "654321" {
			t.Fatalf("value %q", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the waiter did not wake")
	}
}

func TestCodeEnds(t *testing.T) {
	c, now := testCodes()
	a, _ := c.Open("x", "conv-a", "", 6, time.Minute)
	if !c.Cancel(a.ID) {
		t.Fatal("cancel")
	}
	if state, _ := c.Wait(context.Background(), a.ID, "conv-a", time.Millisecond); state != CodeCancelled {
		t.Fatalf("cancelled reads %q", state)
	}
	if ok, _ := c.Put(a.ID, "123456"); ok {
		t.Fatal("a cancelled ask took a code")
	}

	b, _ := c.Open("x", "conv-a", "", 6, time.Minute)
	*now = now.Add(codeAlive)
	if len(c.Pending()) != 0 {
		t.Fatal("an ask nobody waits on is still shown")
	}
	if state, _ := c.Wait(context.Background(), b.ID, "conv-a", time.Millisecond); state != CodeGone {
		t.Fatalf("abandoned reads %q", state)
	}

	d, _ := c.Open("x", "conv-a", "", 6, time.Minute)
	*now = now.Add(10 * time.Second)
	c.Wait(context.Background(), d.ID, "conv-a", time.Millisecond)
	*now = now.Add(51 * time.Second)
	if state, _ := c.Wait(context.Background(), d.ID, "conv-a", time.Millisecond); state != CodeExpired {
		t.Fatalf("past its time reads %q", state)
	}

	e, _ := c.Open("x", "conv-a", "", 6, time.Minute)
	_, _ = c.Put(e.ID, "111111")
	*now = now.Add(codeKept)
	if state, v := c.Wait(context.Background(), e.ID, "conv-a", time.Millisecond); state != CodeGone || v != "" {
		t.Fatalf("an untaken code outlived its time: %q %q", state, v)
	}
}

func TestCodeRefusals(t *testing.T) {
	c, _ := testCodes()
	if _, err := c.Open("x", "", "", 6, time.Minute); ReasonOf(err) != ReasonNoChat {
		t.Fatalf("no chat: %v", err)
	}
	if _, err := c.Open("x", "conv-a", "", 3, time.Minute); ReasonOf(err) != ReasonBadCode {
		t.Fatalf("three digits: %v", err)
	}
	for i := range 6 {
		if _, err := c.Open("x", "conv-a", "", 6, time.Minute); err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
	}
	if _, err := c.Open("x", "conv-a", "", 6, time.Minute); ReasonOf(err) != ReasonRateLimited {
		t.Fatalf("seventh: %v", err)
	}
}
