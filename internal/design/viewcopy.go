package design

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A recording (records/<name>.mp4) is shot at twice the pixels of the final video (2880×1620) and is only
// for the final render. A view of the project, from the hub's snapshot or a phone, gets a light copy: one file
// beside it, records/<name>.view.mp4, 1440 lines high, no sound, about five times smaller. The copy is a project file,
// so it syncs to the hub with the rest of the snapshot; the public route answers a request for the clip from it.

// FullClipsHeader marks a request of the final render: it gets a recording itself, not its light copy.
const FullClipsHeader = "X-Tk-Full-Clips"

const (
	viewCopySuffix = ".view.mp4"
	viewCopyHeight = 1440
	viewCopyQuiet  = 4 * time.Second
	viewCopyLimit  = 10 * time.Minute
)

type viewCopies struct {
	mu     sync.Mutex
	timer  *time.Timer
	stop   func()
	cancel context.CancelFunc
	run    sync.Mutex // one copy at a time: ffmpeg is niced, but a machine has few cores
}

// startViewCopies watches the recordings: a new or changed clip gets its light copy after a quiet moment, and the
// clips that have none yet get theirs now.
func (l *Library) startViewCopies() {
	v := &l.views
	v.mu.Lock()
	if v.stop != nil {
		v.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel
	v.mu.Unlock()
	stop, _ := l.Subscribe(func(rel string) {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 4 || parts[1] != ProjectDirname || !isRecordClip(parts[2:]) {
			return
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.timer != nil {
			v.timer.Stop()
		}
		v.timer = time.AfterFunc(viewCopyQuiet, func() { l.sweepViewCopies(ctx) })
	})
	v.mu.Lock()
	v.stop = stop
	if v.stop == nil {
		v.stop = func() {}
	}
	v.timer = time.AfterFunc(viewCopyQuiet, func() { l.sweepViewCopies(ctx) })
	v.mu.Unlock()
}

func (l *Library) stopViewCopies() {
	v := &l.views
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.stop != nil {
		v.stop()
		v.stop = nil
	}
	if v.timer != nil {
		v.timer.Stop()
		v.timer = nil
	}
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
}

// sweepViewCopies makes the light copy of every recording whose copy is missing or older than the clip.
func (l *Library) sweepViewCopies(ctx context.Context) {
	root := l.Root()
	if root == "" {
		return
	}
	bin := DeckFFmpeg()
	if bin == "" {
		return
	}
	clips, _ := filepath.Glob(filepath.Join(root, "*", ProjectDirname, RecordsDir, "*.mp4"))
	v := &l.views
	v.run.Lock()
	defer v.run.Unlock()
	for _, clip := range clips {
		if ctx.Err() != nil {
			return
		}
		name := filepath.Base(clip)
		if strings.HasSuffix(strings.ToLower(name), viewCopySuffix) {
			continue
		}
		light := strings.TrimSuffix(clip, filepath.Ext(clip)) + viewCopySuffix
		if !viewCopyInside(root, clip) || !viewCopyStale(clip, light) {
			continue
		}
		_ = makeViewCopy(ctx, bin, clip, light)
	}
}

// viewCopyInside: the clip is a regular file (not a link) whose folder, once every link on the way is resolved, is exactly
// <library>/<project>/project/records of the resolved library root, so ffmpeg never reads a file from elsewhere.
func viewCopyInside(root, clip string) bool {
	st, err := os.Lstat(clip)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	dirReal, err := filepath.EvalSymlinks(filepath.Dir(clip))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootReal, dirReal)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) == 3 && IsLibraryID(parts[0]) && parts[1] == ProjectDirname && parts[2] == RecordsDir
}

// viewCopyStale: the clip has no copy, or changed after its copy was made.
func viewCopyStale(clip, light string) bool {
	cs, err := os.Stat(clip)
	if err != nil || !cs.Mode().IsRegular() || cs.Size() == 0 {
		return false
	}
	ls, err := os.Stat(light)
	return err != nil || ls.Size() == 0 || ls.ModTime().Before(cs.ModTime())
}

// viewCopyArgs is the ffmpeg command line of a light copy: 1440 lines at most (a smaller clip keeps its size), the
// picture only, the first frames at once (faststart) so a view starts before the file is loaded.
func viewCopyArgs(clip, out string) []string {
	// The input is pinned to an MP4 file: no other demuxer, no network or playlist address.
	return []string{"-y", "-nostdin", "-loglevel", "error", "-protocol_whitelist", "file", "-f", "mov", "-i", "file:" + clip,
		"-vf", `scale=-2:min(` + strconv.Itoa(viewCopyHeight) + `\,ih):flags=lanczos`,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "30", "-pix_fmt", "yuv420p", "-an",
		"-movflags", "+faststart", "-f", "mp4", out}
}

// makeViewCopy writes the copy next to a hidden temporary file and renames it into place, so a half-made copy is
// never read (a hidden name does not sync either).
func makeViewCopy(ctx context.Context, bin, clip, light string) error {
	ctx, cancel := context.WithTimeout(ctx, viewCopyLimit)
	defer cancel()
	// A fresh file of a random name that only this call made: nothing planted at a known name is written through.
	part, err := os.CreateTemp(filepath.Dir(light), "."+filepath.Base(light)+".*.part")
	if err != nil {
		return err
	}
	tmp := part.Name()
	part.Close()
	defer os.Remove(tmp)
	cmd := recNice(ctx, bin, viewCopyArgs(clip, tmp)...)
	if err := cmd.Run(); err != nil {
		return err
	}
	st, err := os.Stat(tmp)
	if err != nil || st.Size() == 0 {
		return err
	}
	return os.Rename(tmp, light)
}
