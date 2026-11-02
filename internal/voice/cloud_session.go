package voice

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"sync"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/voice/cloud"
	"github.com/ly-solutions/harnessly/internal/voice/dict"
)

// The engines a page may name in `start`. Anything but "local" sends the audio to the provider, and only a person who
// picked that model in Settings makes the page name it.
const (
	engineOpenAI = "openai"
	engineYandex = "yandex"
)

var (
	msgNoCloudKey = locale.T("No key is saved for this recognition model: add it in Settings, Models and keys.", "Для этой модели распознавания не сохранён ключ: добавьте его в «Модели и ключи».")
	msgBadEngine  = locale.T("Unknown recognition model.", "Неизвестная модель распознавания.")
)

// parseDict reads the `dict` of a start message: [{word, heard: []}…]. A malformed entry is dropped.
func parseDict(v any) []dict.Entry {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []dict.Entry
	for _, item := range list {
		o, ok := item.(*jsvalue.Object)
		if !ok {
			continue
		}
		w, _ := o.Get("word")
		word, _ := w.(string)
		e := dict.Entry{Word: word}
		for _, h := range o.Array("heard") {
			if hs, ok := h.(string); ok {
				e.Heard = append(e.Heard, hs)
			}
		}
		out = append(out, e)
	}
	return dict.Clean(out)
}

// cloudDial prepares the connection of a cloud engine: the saved key is read now, so a missing one is told at the start.
func (s *Server) cloudDial(engine string, words []dict.Entry, lang string) (dialFunc, *failure) {
	fn := s.cfg.Cloud[engine]
	if fn == nil && engine == engineOpenAI {
		fn = cloud.OpenAI
	}
	if fn == nil && engine == engineYandex {
		fn = cloud.Yandex
	}
	if fn == nil || (engine != engineOpenAI && engine != engineYandex) {
		return nil, &failure{msgBadEngine.String(), "failed"}
	}
	if s.cfg.Credentials == nil {
		return nil, &failure{msgNoCloudKey.String(), "failed"}
	}
	key, folder, err := s.cfg.Credentials(engine)
	if err != nil || key == "" {
		return nil, &failure{msgNoCloudKey.String(), "failed"}
	}
	opts := cloud.Options{Lang: lang, Key: key, Folder: folder, Hints: dict.Hints(words)}
	return func(ctx context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go serveCloud(ctx, server, fn, opts)
		return client, nil
	}, nil
}

// serveCloud speaks the recognition service's protocol (newline-separated JSON: cmd start | audio | stop in, type ready |
// partial | final | error out) on conn, and recognizes with fn behind it, so a session drives a cloud engine like the
// machine's own service.
func serveCloud(ctx context.Context, conn net.Conn, fn cloud.Func, opts cloud.Options) {
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make(chan []byte, 64)
	var mu sync.Mutex
	closed := false
	write := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return
		}
		select {
		case out <- jsonLine(v):
		case <-ctx.Done():
		}
	}
	written := make(chan struct{})
	go func() {
		defer close(written)
		for b := range out {
			if _, err := conn.Write(b); err != nil {
				cancel()
				for range out {
				}
				return
			}
		}
	}()
	pcm := make(chan []byte, 256)
	go func() {
		defer close(pcm)
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for sc.Scan() {
			var msg struct {
				Cmd string `json:"cmd"`
				PCM string `json:"pcm"`
			}
			if json.Unmarshal(sc.Bytes(), &msg) != nil {
				continue
			}
			switch msg.Cmd {
			case "audio":
				if raw, err := base64.StdEncoding.DecodeString(msg.PCM); err == nil && len(raw) > 0 {
					select {
					case pcm <- raw:
					case <-ctx.Done():
						return
					}
				}
			case "stop":
				return
			}
		}
		// The session hung up before it said stop: nothing is recognized further.
		if ctx.Err() == nil {
			cancel()
		}
	}()
	fn(ctx, opts, pcm, cloud.Events{
		Ready:   func() { write(jsvalue.NewObject("type", "ready")) },
		Partial: func(t string) { write(jsvalue.NewObject("type", "partial", "text", t)) },
		Final:   func(t string) { write(jsvalue.NewObject("type", "final", "text", t)) },
		Fail: func(m string) {
			write(jsvalue.NewObject("type", "error", "message", m, "code", "failed"))
		},
	})
	// The last message reaches the session before the connection closes.
	mu.Lock()
	closed = true
	close(out)
	mu.Unlock()
	<-written
}
