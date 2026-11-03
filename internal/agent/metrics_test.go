//go:build linux

package agent

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const procStat1 = "cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 50 0 50 350 50 0 0 0 0 0\nintr 1\n"
const procStat2 = "cpu  200 0 200 750 150 0 0 0 0 0\n" // +200 busy of +300 total

func TestParseProcStat(t *testing.T) {
	busy, total, err := parseProcStat(procStat1)
	if err != nil || busy != 200 || total != 1000 {
		t.Fatalf("busy %d total %d err %v", busy, total, err)
	}
	if _, _, err := parseProcStat("intr 1\n"); err == nil {
		t.Fatal("no cpu line was accepted")
	}
	if _, _, err := parseProcStat("cpu  a b c d e\n"); err == nil {
		t.Fatal("garbage was accepted")
	}
	// guest columns are already in user and are not added twice.
	busy, total, _ = parseProcStat("cpu  100 0 100 700 100 0 0 0 50 50\n")
	if busy != 200 || total != 1000 {
		t.Fatalf("guest counted twice: busy %d total %d", busy, total)
	}
}

func TestParseMeminfo(t *testing.T) {
	total, avail, err := parseMeminfo("MemTotal:       8000 kB\nMemFree:        1000 kB\nMemAvailable:   4000 kB\n")
	if err != nil || total != 8000*1024 || avail != 4000*1024 {
		t.Fatalf("%d %d %v", total, avail, err)
	}
	// An old kernel has no MemAvailable.
	total, avail, _ = parseMeminfo("MemTotal: 8000 kB\nMemFree: 1000 kB\nBuffers: 500 kB\nCached: 1500 kB\n")
	if avail != 3000*1024 || total != 8000*1024 {
		t.Fatalf("fallback: %d", avail)
	}
	if _, _, err := parseMeminfo("MemFree: 1 kB\n"); err == nil {
		t.Fatal("no MemTotal was accepted")
	}
}

func TestParseVMStatAndPS(t *testing.T) {
	vm := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free:                               100.\nPages active:                             999.\nPages inactive:                           50.\nPages speculative:                        10.\nPages purgeable:                          5.\n"
	avail, err := parseVMStat(vm)
	if err != nil || avail != 165*16384 {
		t.Fatalf("%d %v", avail, err)
	}
	if _, err := parseVMStat("nothing"); err == nil {
		t.Fatal("empty vm_stat was accepted")
	}
	sum, err := parsePSCPU(" 12.5\n  0.0\n 7.5\n\n")
	if err != nil || sum != 20 {
		t.Fatalf("%v %v", sum, err)
	}
	if _, err := parsePSCPU("x\n"); err == nil {
		t.Fatal("garbage was accepted")
	}
}

func TestCPUMeter(t *testing.T) {
	var m cpuMeter
	if _, err := m.pct(200, 1000); !errors.Is(err, errNoBaseline) {
		t.Fatalf("the first read is a baseline: %v", err)
	}
	v, err := m.pct(400, 1300)
	if err != nil || v < 66.6 || v > 66.7 {
		t.Fatalf("%v %v", v, err)
	}
	if _, err := m.pct(100, 1400); err == nil {
		t.Fatal("ticks that went backwards were accepted")
	}
}

// fakeSys is a machine that says what it is told.
type fakeSys struct {
	cpu         []float64
	cpuErr      error
	total, free uint64
	memErr      error
	dTotal      uint64
	dUsed       uint64
	diskErr     error
	hw          hwFacts
}

func (f *fakeSys) CPUPct() (float64, error) {
	if f.cpuErr != nil {
		return 0, f.cpuErr
	}
	v := f.cpu[0]
	if len(f.cpu) > 1 {
		f.cpu = f.cpu[1:]
	}
	return v, nil
}
func (f *fakeSys) Memory() (uint64, uint64, error)     { return f.total, f.free, f.memErr }
func (f *fakeSys) Disk(string) (uint64, uint64, error) { return f.dTotal, f.dUsed, f.diskErr }
func (f *fakeSys) Hardware(string) hwFacts             { return f.hw }

func TestSamplerSample(t *testing.T) {
	at := time.UnixMilli(1_700_000_000_000)
	three := 3
	f := &fakeSys{cpu: []float64{41.26}, total: 1000, free: 250, dTotal: 200, dUsed: 50}
	s := &sampler{sys: f, path: "/", chats: func() *int { return &three }, now: func() time.Time { return at }}
	got, ok := s.sample()
	if !ok || got.CPUPct == nil || *got.CPUPct != 41.3 || *got.RAMPct != 75 || *got.DiskPct != 25 || *got.ChatsNow != 3 || got.At != 1_700_000_000_000 {
		t.Fatalf("%+v %v", got, ok)
	}
}

func TestSamplerLeavesOutWhatItCannotRead(t *testing.T) {
	f := &fakeSys{cpuErr: errNoBaseline, memErr: errors.New("x"), dTotal: 100, dUsed: 100}
	s := &sampler{sys: f, path: "/", chats: func() *int { return nil }, now: time.Now}
	got, ok := s.sample()
	if !ok || got.CPUPct != nil || got.RAMPct != nil || *got.DiskPct != 100 || got.ChatsNow != nil {
		t.Fatalf("%+v", got)
	}
	f.diskErr = errors.New("y")
	if _, ok := s.sample(); ok {
		t.Fatal("a sample of nothing was made")
	}
}

func TestSamplerHardware(t *testing.T) {
	f := &fakeSys{hw: hwFacts{Cores: 4, RAMBytes: 8 << 30, DiskBytes: 100 << 30, Model: "Example CPU", Laptop: true}}
	s := &sampler{sys: f, path: "/"}
	hw := s.hardware("linux", "amd64")
	if hw == nil || hw.Cores != 4 || hw.RAMBytes != 8<<30 || hw.Model != "Example CPU" || !hw.Laptop || hw.OS != "linux" {
		t.Fatalf("%+v", hw)
	}
	if (&sampler{sys: &fakeSys{}, path: "/"}).hardware("x", "y") != nil {
		t.Fatal("an unknown machine claimed hardware")
	}
}

func TestLinuxReaderOnAFakeRoot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proc/stat", procStat1)
	write("proc/meminfo", "MemTotal: 2000 kB\nMemAvailable: 500 kB\n")
	write("proc/cpuinfo", "model name : Example CPU\n")
	write("sys/class/dmi/id/chassis_type", "10\n")
	r := linuxReader{root: root, cpu: &cpuMeter{}}
	if _, err := r.CPUPct(); !errors.Is(err, errNoBaseline) {
		t.Fatalf("first read: %v", err)
	}
	write("proc/stat", procStat2)
	v, err := r.CPUPct()
	if err != nil || v < 66.6 || v > 66.7 {
		t.Fatalf("cpu %v %v", v, err)
	}
	if total, avail, err := r.Memory(); err != nil || total != 2000*1024 || avail != 500*1024 {
		t.Fatalf("mem %d %d %v", total, avail, err)
	}
	hw := r.Hardware(root)
	if hw.Model != "Example CPU" || !hw.Laptop || hw.RAMBytes != 2000*1024 || hw.DiskBytes <= 0 || hw.Cores < 1 {
		t.Fatalf("%+v", hw)
	}
	if total, used, err := r.Disk(root); err != nil || total == 0 || used > total {
		t.Fatalf("disk %d %d %v", total, used, err)
	}
	// The real machine, whatever it is, gives sane numbers.
	real := newSysReader()
	if total, avail, err := real.Memory(); err != nil || avail > total {
		t.Fatalf("real memory %d %d %v", total, avail, err)
	}
}

func TestLinkSubscribedIsStrict(t *testing.T) {
	l := &link{}
	if !l.wants(hubproto.EventMetricsSample) || l.subscribed(hubproto.EventMetricsSample) {
		t.Fatal("a link without a list: wants everything, subscribed to nothing")
	}
	l.subscribe([]string{hubproto.EventJob})
	if l.subscribed(hubproto.EventMetricsSample) || !l.subscribed(hubproto.EventJob) {
		t.Fatal("the list is not read")
	}
	l.subscribe([]string{hubproto.EventMetricsSample})
	if !l.subscribed(hubproto.EventMetricsSample) {
		t.Fatal("subscribed event not seen")
	}
	if strings.Contains(hubproto.EventMetricsSample, " ") {
		t.Fatal("event name")
	}
}
