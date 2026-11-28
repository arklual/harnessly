package connect

import (
	"testing"
)

func TestChatRoutes(t *testing.T) {
	dir := t.TempDir()
	s := &Server{Dir: dir}
	s.init()
	s.mu.Lock()
	s.devices["d-1"] = &DeviceRec{ID: "d-1", Name: "laptop"}
	s.mu.Unlock()
	if _, err := s.AddFolder("laptop", "/home/me"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChat("c1", "laptop", "relative"); err == nil {
		t.Fatal("a relative folder")
	}
	if _, err := s.SetChat("c1", "nope", "/x"); err == nil {
		t.Fatal("an unknown device")
	}
	if _, err := s.SetChat("c1", "laptop", "/home/me/proj/"); err != nil {
		t.Fatal(err)
	}
	// A switched chat runs in its device folder whatever its folder here; another chat goes by its folder.
	if d, dir, cwd, ok := s.RouteChat("c1", "/srv/proj"); !ok || d.ID != "d-1" || dir != "/home/me/proj" || cwd != dir {
		t.Fatalf("%+v %q %q %v", d, dir, cwd, ok)
	}
	if _, dir, cwd, ok := s.RouteChat("c2", "/home/me/x"); !ok || dir != "/home/me" || cwd != "/home/me/x" {
		t.Fatalf("folder chat: %q %q %v", dir, cwd, ok)
	}
	if _, _, _, ok := s.RouteChat("c2", "/srv/proj"); ok {
		t.Fatal("a server chat routed")
	}
	// Kept across a restart; back on the server, gone.
	again := &Server{Dir: dir}
	if c, ok := again.ChatOf("c1"); !ok || c.Dir != "/home/me/proj" {
		t.Fatalf("reloaded %+v %v", c, ok)
	}
	if !again.ClearChat("c1") || again.ClearChat("c1") {
		t.Fatal("clear")
	}
	if _, _, _, ok := again.RouteChat("c1", "/srv/proj"); ok {
		t.Fatal("still routed")
	}
	// Revoking the device puts its chats back.
	_, _ = s.SetChat("c3", "laptop", "/home/me")
	s.Revoke("laptop")
	if _, ok := s.ChatOf("c3"); ok {
		t.Fatal("revoked device keeps a chat")
	}
}
