package connect

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ChatRoute puts one chat on a device, whatever its folder on this machine: the person switched it there. Dir is the
// folder on the device it works in; switched back, the chat goes on in its own folder here.
type ChatRoute struct {
	Conv   string    `json:"conv"`
	Device string    `json:"device"`
	Dir    string    `json:"dir"`
	At     time.Time `json:"at"`
}

// ChatOf is the device a chat was switched to, if any.
func (s *Server) ChatOf(conv string) (ChatRoute, bool) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.chats[conv]
	return c, ok
}

// SetChat switches a chat to a device folder (dir in the device's own form).
func (s *Server) SetChat(conv, device, dir string) (ChatRoute, error) {
	s.init()
	dir = path.Clean(strings.TrimSpace(dir))
	if conv == "" {
		return ChatRoute{}, errors.New("нет чата")
	}
	if !strings.HasPrefix(dir, "/") {
		return ChatRoute{}, errors.New("нужен полный путь на устройстве (/home/… или /c/Users/… для Windows)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.find(device)
	if d == nil {
		return ChatRoute{}, errors.New("нет такого устройства")
	}
	c := ChatRoute{Conv: conv, Device: d.ID, Dir: dir, At: time.Now().UTC()}
	s.chats[conv] = c
	s.saveChats()
	return c, nil
}

// ClearChat puts a chat back on this machine; false when it was not switched.
func (s *Server) ClearChat(conv string) bool {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.chats[conv]; !ok {
		return false
	}
	delete(s.chats, conv)
	s.saveChats()
	return true
}

// RouteChat is where the chat runs: the device it was switched to, else the device folder its cwd lies in.
func (s *Server) RouteChat(conv, cwd string) (DeviceRec, string, string, bool) {
	if c, ok := s.ChatOf(conv); ok && conv != "" {
		s.mu.Lock()
		d := s.find(c.Device)
		var rec DeviceRec
		if d != nil {
			rec = *d
			rec.Pub = nil
			_, rec.Online = s.links[d.ID]
		}
		s.mu.Unlock()
		if d != nil {
			return rec, c.Dir, c.Dir, true
		}
	}
	d, dir, ok := s.Route(cwd)
	return d, dir, cwd, ok
}

func (s *Server) loadChats() {
	s.chats = map[string]ChatRoute{}
	if raw, err := os.ReadFile(filepath.Join(s.Dir, "chats.json")); err == nil {
		var list []ChatRoute
		if json.Unmarshal(raw, &list) == nil {
			for _, c := range list {
				s.chats[c.Conv] = c
			}
		}
	}
}

// saveChats writes the switched chats; s.mu is held.
func (s *Server) saveChats() {
	list := make([]ChatRoute, 0, len(s.chats))
	for _, c := range s.chats {
		list = append(list, c)
	}
	raw, _ := json.MarshalIndent(list, "", "  ")
	_ = os.MkdirAll(s.Dir, 0o700)
	tmp := filepath.Join(s.Dir, ".chats.json.tmp")
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(s.Dir, "chats.json"))
	}
}
