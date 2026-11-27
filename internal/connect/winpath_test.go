package connect

import "testing"

func TestRewriteWinPaths(t *testing.T) {
	for in, want := range map[string]string{
		`File "C:\Users\m\proj\a.py", line 3`: `File "/c/Users/m/proj/a.py", line 3`,
		`at D:\work\x.ts:10:5`:                `at /d/work/x.ts:10:5`,
		`cd C:/Users/m && ls`:                 `cd /c/Users/m && ls`,
		`ratio 3:4 and http://x.y/z stay`:     `ratio 3:4 and http://x.y/z stay`,
		`error TS2304 in C:\a b\c.ts`:         `error TS2304 in /c/a b\c.ts`,
	} {
		if got := string(RewriteWinPaths([]byte(in))); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	var got []byte
	w := &lineRewriter{out: func(b []byte) { got = append(got, b...) }}
	w.Write([]byte(`open C:\Us`))
	w.Write([]byte("ers\\m\\f.txt\nnext"))
	w.flush()
	if string(got) != "open /c/Users/m/f.txt\nnext" {
		t.Fatalf("split: %q", got)
	}
}
