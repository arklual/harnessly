package remote

import "testing"

func TestOf(t *testing.T) {
	Set([]Folder{{Device: "a", Dir: "/Users/m"}, {Device: "b", Dir: "/Users/m/proj"}, {Device: "w", Dir: "/c/Users/m"}})
	defer Set(nil)
	for path, want := range map[string]string{"/Users/m/proj/x": "b", "/Users/m": "a", "/Users/m/other": "a",
		"/Users/mm": "", "/c/Users/m/x/": "w", "/home/ubuntu": ""} {
		f, ok := Of(path)
		if f.Device != want || ok != (want != "") {
			t.Errorf("Of(%s) = %v %v, want %q", path, f, ok, want)
		}
	}
}
