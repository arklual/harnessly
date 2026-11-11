package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatmove"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// chatResumeOn: the machine keeps its chats' resume snapshots in the hub's store and continues the person's chats of
// another machine from theirs (RUNTIME_CHAT_RESUME=hub, HRN-1040, docs/hub/resume.md). Off, nothing is sent and the
// move asks for both machines on, as before.
func chatResumeOn() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("RUNTIME_CHAT_RESUME")), "hub")
}

// newChatResume is the person's resume room and key for the move service; nil when the switch is off.
func newChatResume(d deps) *chatmove.Resume {
	if !chatResumeOn() {
		return nil
	}
	keys := &chatmove.ResumeKeys{Safe: agent.LocalSafe{DataDir: d.DataDir, TokenHeader: os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")},
		Folder: agent.SafeOwnFolder}
	return &chatmove.Resume{Store: resumeRoom{}, Key: keys.Key}
}

// errNoHubStore: the runtime has no link to a hub's store (yet).
var errNoHubStore = errors.New("no hub's store linked")

// resumeRoom is the resume collection of the runtime's link to the hub's store, read when it is used: the link is
// opened after the conversations' runtime.
type resumeRoom struct{}

func (resumeRoom) client() (*filesync.Client, error) {
	l := linked.Load()
	if l == nil || l.Kind() != storelink.KindHub {
		return nil, errNoHubStore
	}
	return l.Client(filestore.KindResume), nil
}

func (r resumeRoom) Current(ctx context.Context, path string) (*filestore.Entry, error) {
	c, err := r.client()
	if err != nil {
		return nil, err
	}
	return c.Current(ctx, path)
}

func (r resumeRoom) GetBlob(ctx context.Context, sha string, w io.Writer) error {
	c, err := r.client()
	if err != nil {
		return err
	}
	return c.GetBlob(ctx, sha, w)
}

func (r resumeRoom) Put(ctx context.Context, path string, data []byte, author string) (filestore.Entry, error) {
	c, err := r.client()
	if err != nil {
		return filestore.Entry{}, err
	}
	return c.Put(ctx, path, data, author)
}

// runChatResume keeps the snapshots once the runtime is linked to a hub whose store has the resume collection; an
// older hub is asked again every ten minutes, since it may be upgraded.
func runChatResume(ctx context.Context, svc *chatmove.Service, list func() []string, dataDir string) {
	var l *storelink.Link
	for l == nil {
		if l = linked.Load(); l != nil && l.Kind() == storelink.KindHub && l.MachineID() != "" {
			break
		}
		l = nil
		if !sleepCtx(ctx, 30*time.Second) {
			return
		}
	}
	if err := l.Ready(ctx, 10*time.Minute); err != nil {
		return
	}
	told := false
	for {
		ok, err := l.Serves(ctx, filestore.KindResume)
		if ok {
			break
		}
		if !told {
			slog.Info("chat resume: the hub's store has no resume collection yet, nothing is sent", "err", err)
			told = true
		}
		if !sleepCtx(ctx, 10*time.Minute) {
			return
		}
	}
	slog.Info("chat resume: the chats' snapshots go to the hub's store")
	(&chatmove.ResumeUploader{Service: svc, Machine: l.MachineID(), List: list,
		State: filepath.Join(dataDir, "v2", "chat-resume.json"), Log: slog.Default()}).Run(ctx)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
