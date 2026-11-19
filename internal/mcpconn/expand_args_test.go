package mcpconn

import (
	"slices"
	"testing"
)

// A field's value goes into the command line where an argument holds {FIELD} (a SQLite file's path); a field left
// empty drops the argument.
func TestExpandArgs(t *testing.T) {
	fields := []EnvField{{Name: "DB"}, {Name: "FLAG", Optional: true}}
	cmd := []string{"uvx", "mcp-server-sqlite==1", "--db-path", "{DB}", "{FLAG}", "--x={DB}"}
	got := expandArgs(cmd, fields, map[string]string{"DB": "/data/app.db"})
	want := []string{"uvx", "mcp-server-sqlite==1", "--db-path", "/data/app.db", "--x=/data/app.db"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if cmd[3] != "{DB}" {
		t.Fatal("the catalog's own command was changed")
	}
}
