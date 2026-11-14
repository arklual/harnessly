package providers

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// with an OffFile the GPT switch is on until the person turns it off, its proxy starts at the first chat,
// and the person's «off» is kept across restarts.
func TestTheGPTSwitchIsOnUntilTurnedOff(t *testing.T) {
	g, auth := newGPT(t)
	g.OffFile = filepath.Join(filepath.Dir(g.OnFile), "gpt.off")
	if !g.On() || !g.Ready() {
		t.Fatal("not on by default")
	}
	if _, _, ok := g.Endpoint(); ok {
		t.Fatal("an endpoint before the first chat")
	}
	if err := g.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := g.Endpoint(); !ok {
		t.Fatal("no endpoint after Ensure")
	}

	if err := g.SetOn(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	g2 := &GPTProxy{Dir: g.Dir, OnFile: g.OnFile, OffFile: g.OffFile, Login: g.Login, RelayBase: g.RelayBase,
		Command: fakeProxyCommand, Install: g.Install}
	t.Cleanup(g2.Close)
	if g2.On() || g2.Ready() || fileExists(g2.OnFile) || !fileExists(g2.OffFile) {
		t.Fatal("the person's off was not kept")
	}
	if err := g2.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := g2.Endpoint(); ok {
		t.Fatal("Ensure started a switch turned off")
	}
	if err := g2.SetOn(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !g2.On() || fileExists(g2.OffFile) || !fileExists(g2.OnFile) {
		t.Fatal("on again did not take")
	}

	// Without a Codex sign-in by subscription the switch is on but not ready, and nothing is started.
	codexAuth(t, auth, "apikey", "a", time.Now().Add(time.Hour), true)
	if g2.Ready() {
		t.Fatal("ready without the subscription")
	}
}

func TestTheAutoSwitchIsOnUntilTurnedOff(t *testing.T) {
	dir := t.TempDir()
	r := &Router{Dir: filepath.Join(dir, "auto-router"), OnFile: filepath.Join(dir, "auto.on"),
		OffFile: filepath.Join(dir, "auto.off"), Base: "http://x" + RouterPath, Store: newStore(t)}
	if !r.On() {
		t.Fatal("not on by default")
	}
	if _, keyFile, ok := r.Endpoint(); !ok || !fileExists(keyFile) {
		t.Fatal("the key of a switch on by default was not made")
	}
	if err := r.SetOn(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if r.On() {
		t.Fatal("off did not take")
	}
	if _, _, ok := r.Endpoint(); ok {
		t.Fatal("an endpoint while off")
	}
}
