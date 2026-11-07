package apps

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The generated files match the surface; UPDATE_SURFACE=1 writes them. A hand edit of one of them fails here, in CI.
func TestSurfaceFilesAreCurrent(t *testing.T) {
	root := filepath.Join("..", "..")
	for path, want := range SurfaceFiles() {
		full := filepath.Join(root, path)
		if os.Getenv("UPDATE_SURFACE") == "1" {
			if err := os.WriteFile(full, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("%s: %v (UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent writes it)", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from internal/apps/surface.go: run UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent", path)
		}
	}
}

// Every right a method needs is a right of the surface, and the check and the bridge read the same table.
func TestSurfaceIsConsistent(t *testing.T) {
	names := PermissionNames()
	for _, m := range surfaceMethods {
		if m.Permission != "" && !slices.Contains(names, m.Permission) {
			t.Errorf("%s needs %q, which is no right", m.Name, m.Permission)
		}
		if need, ok := RequiredPermission(m.Name); !ok || need != m.Permission {
			t.Errorf("%s: bridge says %q %v", m.Name, need, ok)
		}
	}
	for _, p := range []string{"chat.prompt", "storage", "sinopia.kind"} {
		if !permissions[p] {
			t.Errorf("%s is not taken", p)
		}
	}
	if !paramPermissions["files.read"] || permissions["files.read"] || !unsupportedPermissions["ai"] {
		t.Error("parameterised or unsupported rights are read wrong")
	}
	// Every tool the host serves is in the catalog.
	for _, tool := range []string{"open", "data_get", "data_set", "settings_get", "ask", "widget_show", "widget_render", "catalog", "page_tools", "page_call"} {
		if !slices.ContainsFunc(surfaceTools, func(s SurfaceTool) bool { return s.Name == tool }) {
			t.Errorf("tool %s is not in the catalog", tool)
		}
	}
}
