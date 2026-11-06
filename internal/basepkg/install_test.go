package basepkg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fake is a machine: the binaries on PATH, what each install answers, the free disk and the load.
type fake struct {
	have   map[string]bool
	answer func(args []string) (string, error) // nil: the install puts the item's binaries in place
	free   uint64
	busy   bool
	ran    [][]string
	env    [][]string
}

func (f *fake) installer() *Installer {
	return &Installer{
		Manager: Apt,
		Run: func(_ context.Context, env, args []string) (string, error) {
			f.ran = append(f.ran, args)
			f.env = append(f.env, env)
			if f.answer != nil {
				if out, err := f.answer(args); err != nil {
					return out, err
				}
			}
			if args[1] == "install" {
				for _, it := range List {
					if strings.Join(it.Pkgs[Apt], " ") == strings.Join(args[5:], " ") {
						for _, b := range it.Bins {
							f.have[b] = true
						}
					}
				}
			}
			return "", nil
		},
		LookPath: func(b string) (string, error) {
			if f.have[b] {
				return "/usr/bin/" + b, nil
			}
			return "", errors.New("not found")
		},
		Free: func() (uint64, error) { return f.free, nil },
		Busy: func() bool { return f.busy },
		Now:  func() time.Time { return time.Unix(1_700_000_000, 0) },
		Save: func(State) {},
	}
}

func allBut(ids ...string) map[string]bool {
	have := map[string]bool{}
	skip := map[string]bool{}
	for _, id := range ids {
		skip[id] = true
	}
	for _, it := range System() {
		if !skip[it.ID] {
			for _, b := range it.Bins {
				have[b] = true
			}
		}
	}
	return have
}

func status(st State, id string) ItemState {
	for _, s := range st.Items {
		if s.ID == id {
			return s
		}
	}
	return ItemState{}
}

func TestInstallsWhatIsMissingAndNothingElse(t *testing.T) {
	f := &fake{have: allBut("ffmpeg", "jq"), free: 50 << 30}
	st := f.installer().Install(context.Background(), Inbox{})
	if status(st, "ffmpeg").Status != OK || status(st, "jq").Status != OK || status(st, "tmux").Status != OK {
		t.Fatalf("%+v", st.Items)
	}
	// One index refresh, then one install per missing item.
	if len(f.ran) != 3 || f.ran[0][1] != "update" || f.ran[1][len(f.ran[1])-1] != "ffmpeg" {
		t.Fatalf("ran %v", f.ran)
	}
	if st.Running || st.Hash != Hash() {
		t.Fatalf("%+v", st)
	}
}

func TestOneFailureDoesNotStopTheRest(t *testing.T) {
	f := &fake{have: allBut("ffmpeg", "jq"), free: 50 << 30}
	f.answer = func(args []string) (string, error) {
		if args[len(args)-1] == "ffmpeg" {
			return "E: Could not get lock /var/lib/dpkg/lock-frontend", errors.New("exit 100")
		}
		return "", nil
	}
	st := f.installer().Install(context.Background(), Inbox{})
	if s := status(st, "ffmpeg"); s.Status != Failed || !strings.Contains(s.Message, "Could not get lock") {
		t.Fatalf("%+v", s)
	}
	if status(st, "jq").Status != OK {
		t.Fatalf("the next item was not installed: %+v", st.Items)
	}
}

func TestNoNetworkIsNoAccessWithTheHost(t *testing.T) {
	f := &fake{have: allBut("ffmpeg"), free: 50 << 30}
	f.answer = func(args []string) (string, error) {
		if args[1] == "install" {
			return "E: Failed to fetch http://archive.ubuntu.com/ubuntu/pool/ffmpeg.deb  Temporary failure resolving 'archive.ubuntu.com'", errors.New("exit 100")
		}
		return "", nil
	}
	st := f.installer().Install(context.Background(), Inbox{})
	s := status(st, "ffmpeg")
	if s.Status != NoAccess || s.Host != "archive.ubuntu.com" || !strings.Contains(s.Message, "голосового ввода") {
		t.Fatalf("%+v", s)
	}
}

func TestProxyIsUsedOnlyAfterTheDirectWayFails(t *testing.T) {
	f := &fake{have: allBut("jq"), free: 50 << 30}
	f.installer().Install(context.Background(), Inbox{Proxy: "http://proxy:3128"})
	for _, e := range f.env {
		if strings.Contains(strings.Join(e, " "), "proxy:3128") {
			t.Fatalf("the proxy was used while the direct way works: %v", f.env)
		}
	}
	f = &fake{have: allBut("jq"), free: 50 << 30}
	f.answer = func(args []string) (string, error) {
		if args[1] == "install" && !strings.Contains(strings.Join(f.env[len(f.env)-1], " "), "proxy:3128") {
			return "E: Failed to fetch http://deb.debian.org/x  Could not resolve 'deb.debian.org'", errors.New("exit 100")
		}
		return "", nil
	}
	st := f.installer().Install(context.Background(), Inbox{Proxy: "http://proxy:3128"})
	if status(st, "jq").Status != OK || !strings.Contains(strings.Join(f.env[len(f.env)-1], " "), "https_proxy=http://proxy:3128") {
		t.Fatalf("%+v %v", status(st, "jq"), f.env)
	}
}

func TestTheInboxDoesNotReadADevice(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("/dev/zero", filepath.Join(dir, ProxyFile)); err != nil {
		t.Skip(err)
	}
	if in := ReadInbox(dir); in.Proxy != "" {
		t.Fatalf("%+v", in)
	}
}

func TestSkippedLowDiskAndBusy(t *testing.T) {
	f := &fake{have: allBut("jq", "chrome", "libreoffice"), free: 2<<30 + 300<<20, busy: true}
	st := f.installer().Install(context.Background(), Inbox{Skip: map[string]bool{"jq": true}})
	if status(st, "jq").Status != Skipped {
		t.Fatalf("%+v", status(st, "jq"))
	}
	if status(st, "chrome").Status != WaitingDisk || status(st, "libreoffice").Status != WaitingDisk {
		t.Fatalf("%+v", st.Items)
	}
	f = &fake{have: allBut("chrome"), free: 50 << 30, busy: true}
	if s := status(f.installer().Install(context.Background(), Inbox{}), "chrome"); s.Status != Deferred {
		t.Fatalf("%+v", s)
	}
	if len(f.ran) != 0 {
		t.Fatalf("ran %v", f.ran)
	}
}

func TestTheInboxIsReadWithCare(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	_ = os.WriteFile(secret, []byte(`["git"]`), 0o600)
	_ = os.Symlink(secret, filepath.Join(dir, SkipFile)) // a link is not read
	_ = os.WriteFile(filepath.Join(dir, ProxyFile), []byte("http://user:pw@proxy:3128\n"), 0o600)
	if in := ReadInbox(dir); len(in.Skip) != 0 || in.Proxy != "" {
		t.Fatalf("%+v", in)
	}
	_ = os.Remove(filepath.Join(dir, SkipFile))
	_ = os.WriteFile(filepath.Join(dir, SkipFile), []byte(`["chrome","rm -rf /"]`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, ProxyFile), []byte("socks5://10.0.0.1:1080\n"), 0o600)
	in := ReadInbox(dir)
	if !in.Skip["chrome"] || len(in.Skip) != 1 || in.Proxy != "socks5://10.0.0.1:1080" {
		t.Fatalf("%+v", in)
	}
}

func TestCheckProxy(t *testing.T) {
	for raw, ok := range map[string]bool{
		"http://proxy:3128": true, "https://p.example.com": true, "socks5h://1.2.3.4:1080": true,
		"ftp://p": false, "http://u:p@p:1": false, "proxy:3128": false, "http://p:1/x": false, "": false,
	} {
		if _, err := CheckProxy(raw); (err == nil) != ok {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadState(dir); ok {
		t.Fatal("no state yet")
	}
	want := State{Hash: Hash(), Items: []ItemState{{ID: "git", Status: OK}}}
	if err := WriteState(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadState(dir)
	if !ok || got.Hash != want.Hash || got.Items[0].Status != OK {
		t.Fatalf("%+v", got)
	}
}

func TestCommandForThePerson(t *testing.T) {
	ffmpeg, _ := ByID("ffmpeg")
	jq, _ := ByID("jq")
	if c := Command(Apt, []Item{ffmpeg, jq}); c != "sudo apt-get install -y -q --no-install-recommends ffmpeg jq" {
		t.Fatal(c)
	}
	if c := Command(Brew, []Item{jq}); c != "brew install jq" {
		t.Fatal(c)
	}
}
