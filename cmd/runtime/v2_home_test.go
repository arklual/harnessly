package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// The machine's own place: its projects' folder, else home; the conversations stay as they are.
func TestHomePlace(t *testing.T) {
	if got := homePlace(deps{WorkspaceRoot: "/work/p", Home: "/home/a"}); got != "/work/p" {
		t.Errorf("workspace: %q", got)
	}
	if got := homePlace(deps{Home: "/home/a"}); got != "/home/a" {
		t.Errorf("home: %q", got)
	}
	chats := func(id string) (wire.Conversation, bool) {
		if id == "tab-abc" {
			return wire.Conversation{ID: id, Cwd: "/chat"}, true
		}
		return wire.Conversation{}, false
	}
	rec := homeRecord("/work/p", chats)
	if c, ok := rec(wire.HomeConv); !ok || c.Cwd != "/work/p" || c.Session != "" {
		t.Errorf("home record %+v %v", c, ok)
	}
	if c, ok := rec("tab-abc"); !ok || c.Cwd != "/chat" {
		t.Errorf("a chat %+v %v", c, ok)
	}
	if _, ok := homeRecord("", chats)(wire.HomeConv); ok {
		t.Error("a place without a folder is there")
	}
	if !wire.ValidConvID(wire.HomeConv) {
		t.Error("the home place is not a valid id on the wire")
	}
}

// «Файлы» of the start page list and read the machine's own place as a chat's folder.
func TestV2FilesHome(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &v2Files{record: homeRecord(root, func(string) (wire.Conversation, bool) { return wire.Conversation{}, false })}
	ctx := context.Background()
	list, err := f.Dir(ctx, wire.HomeConv, "")
	if err != nil || len(list.Entries) != 1 || list.Entries[0].Path != "notes.md" {
		t.Fatalf("%+v %v", list, err)
	}
	file, err := f.File(ctx, wire.HomeConv, "notes.md")
	if err != nil || file.Content != "hi\n" {
		t.Fatalf("%+v %v", file, err)
	}
	if _, err := f.Dir(ctx, "tab-gone", ""); !errors.Is(err, convo.ErrNotFound) {
		t.Fatalf("another id: %v", err)
	}
}
