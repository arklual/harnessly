package cloud

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

type fakeYandex struct {
	mu     sync.Mutex
	md     metadata.MD
	first  []byte
	chunks int
}

// startFakeYandex serves the SpeechKit method; script drives the server side after the session options arrived.
func startFakeYandex(t *testing.T, script func(ss grpc.ServerStream, f *fakeYandex) error) (*fakeYandex, string) {
	t.Helper()
	f := &fakeYandex{}
	srv := grpc.NewServer(grpc.ForceServerCodecV2(rawCodec{}))
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "speechkit.stt.v3.Recognizer",
		HandlerType: (*any)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName:    "RecognizeStreaming",
			ClientStreams: true,
			ServerStreams: true,
			Handler: func(_ any, ss grpc.ServerStream) error {
				f.mu.Lock()
				f.md, _ = metadata.FromIncomingContext(ss.Context())
				f.mu.Unlock()
				var first []byte
				if err := ss.RecvMsg(&first); err != nil {
					return err
				}
				f.mu.Lock()
				f.first = first
				f.mu.Unlock()
				return script(ss, f)
			},
		}},
	}, struct{}{})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	return f, l.Addr().String()
}

func alt(text string) []byte {
	a := protowire.AppendTag(nil, 2, protowire.BytesType)
	a = protowire.AppendString(a, text)
	return appendMsg(nil, 1, a)
}

func respPartial(text string) []byte { return appendMsg(nil, 4, alt(text)) }

func respFinal(idx int64, text string) []byte {
	b := appendMsg(nil, 2, appendVarint(nil, 5, uint64(idx)))
	return appendMsg(b, 5, alt(text))
}

func respRefine(idx int64, text string) []byte {
	r := appendVarint(nil, 1, uint64(idx))
	r = appendMsg(r, 2, alt(text))
	return appendMsg(nil, 7, r)
}

// drain reads client messages until the client closes its side, counting audio chunks.
func drain(ss grpc.ServerStream, f *fakeYandex) {
	for {
		var m []byte
		if ss.RecvMsg(&m) != nil {
			return
		}
		f.mu.Lock()
		f.chunks++
		f.mu.Unlock()
	}
}

type result struct {
	mu       sync.Mutex
	ready    bool
	partials []string
	finals   []string
	fails    []string
}

func (r *result) events() Events {
	return Events{
		Ready:   func() { r.mu.Lock(); r.ready = true; r.mu.Unlock() },
		Partial: func(s string) { r.mu.Lock(); r.partials = append(r.partials, s); r.mu.Unlock() },
		Final:   func(s string) { r.mu.Lock(); r.finals = append(r.finals, s); r.mu.Unlock() },
		Fail:    func(s string) { r.mu.Lock(); r.fails = append(r.fails, s); r.mu.Unlock() },
	}
}

func fields(t *testing.T, b []byte) map[protowire.Number][]byte {
	t.Helper()
	m := map[protowire.Number][]byte{}
	if err := walk(b, func(n protowire.Number, typ protowire.Type, val []byte, v uint64) {
		if typ == protowire.BytesType {
			m[n] = val
		} else {
			m[n] = protowire.AppendVarint(nil, v)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

func varint(t *testing.T, b []byte) uint64 {
	t.Helper()
	v, n := protowire.ConsumeVarint(b)
	if n < 0 {
		t.Fatal("bad varint")
	}
	return v
}

func TestYandexAssemblesTextAndSendsOptions(t *testing.T) {
	f, addr := startFakeYandex(t, func(ss grpc.ServerStream, f *fakeYandex) error {
		drain(ss, f)
		for _, m := range [][]byte{
			respPartial("привет"),
			respFinal(0, "привет мир"),
			respPartial("два"),
			respRefine(0, "Привет, мир."),
			respFinal(1, "два 3"),
			respPartial("хвост"),
		} {
			if err := ss.SendMsg(m); err != nil {
				return err
			}
		}
		return nil
	})
	pcm := make(chan []byte, 3)
	pcm <- make([]byte, 100)
	pcm <- make([]byte, 40<<10)
	close(pcm)
	var res result
	key := "k" + "ey-test"
	Yandex(context.Background(), Options{Lang: "ru", Key: key, Folder: "b1g-folder", Hints: []string{"x"}, Endpoint: addr}, pcm, res.events())

	if !res.ready {
		t.Fatal("Ready not called")
	}
	if len(res.fails) != 0 || len(res.finals) != 1 {
		t.Fatalf("fails=%v finals=%v", res.fails, res.finals)
	}
	if want := "Привет, мир. два 3 хвост"; res.finals[0] != want {
		t.Fatalf("final = %q, want %q", res.finals[0], want)
	}
	if got := res.partials[0]; got != "привет" {
		t.Fatalf("first partial = %q", got)
	}
	if got := res.partials[len(res.partials)-1]; got != "Привет, мир. два 3 хвост" {
		t.Fatalf("last partial = %q", got)
	}
	if f.chunks != 3 { // 100 B, then 40 KB split in 32 KB + 8 KB
		t.Fatalf("chunks = %d, want 3", f.chunks)
	}
	if got := f.md.Get("authorization"); len(got) != 1 || got[0] != "Api-Key "+key {
		t.Fatalf("authorization = %v", got)
	}
	if got := f.md.Get("x-folder-id"); len(got) != 1 || got[0] != "b1g-folder" {
		t.Fatalf("x-folder-id = %v", got)
	}

	opts := fields(t, fields(t, f.first)[1])
	model := fields(t, opts[1])
	if string(model[1]) != "general" || varint(t, model[5]) != 1 {
		t.Fatalf("model options: %v", model)
	}
	raw := fields(t, fields(t, model[2])[1])
	if varint(t, raw[1]) != 1 || varint(t, raw[2]) != 16000 || varint(t, raw[3]) != 1 {
		t.Fatalf("raw audio: %v", raw)
	}
	if varint(t, fields(t, model[3])[1]) != 1 {
		t.Fatal("text normalization not enabled")
	}
	lr := fields(t, model[4])
	if varint(t, lr[1]) != 1 || string(lr[2]) != "ru-RU" {
		t.Fatalf("language restriction: %v", lr)
	}
	if varint(t, fields(t, fields(t, opts[2])[1])[1]) != 1 {
		t.Fatal("eou classifier is not default")
	}
}

func TestYandexNoFolderHeader(t *testing.T) {
	f, addr := startFakeYandex(t, func(ss grpc.ServerStream, f *fakeYandex) error { drain(ss, f); return nil })
	pcm := make(chan []byte)
	close(pcm)
	var res result
	Yandex(context.Background(), Options{Lang: "auto", Key: "k" + "ey-test", Endpoint: addr}, pcm, res.events())
	if len(f.md.Get("x-folder-id")) != 0 {
		t.Fatalf("folder header sent: %v", f.md)
	}
	if len(res.finals) != 1 || res.finals[0] != "" {
		t.Fatalf("finals = %q fails = %v", res.finals, res.fails)
	}
	lr := fields(t, fields(t, fields(t, f.first)[1])[1])
	if !strings.Contains(string(lr[4]), "en-US") || !strings.Contains(string(lr[4]), "ru-RU") {
		t.Fatalf("auto must whitelist both languages: %q", lr[4])
	}
}

func TestYandexUnauthenticated(t *testing.T) {
	_, addr := startFakeYandex(t, func(ss grpc.ServerStream, f *fakeYandex) error {
		return status.Error(codes.Unauthenticated, "bad key")
	})
	pcm := make(chan []byte)
	var res result
	Yandex(context.Background(), Options{Lang: "ru", Key: "k" + "ey-test", Folder: "f1", Endpoint: addr}, pcm, res.events())
	if len(res.finals) != 0 || len(res.fails) != 1 {
		t.Fatalf("finals=%v fails=%v", res.finals, res.fails)
	}
	if !strings.Contains(res.fails[0], "ключ") || !strings.Contains(res.fails[0], "каталога") {
		t.Fatalf("message = %q", res.fails[0])
	}
	if strings.Contains(res.fails[0], "ey-test") {
		t.Fatal("key leaked into the message")
	}
}

func TestYandexAbortByContext(t *testing.T) {
	_, addr := startFakeYandex(t, func(ss grpc.ServerStream, f *fakeYandex) error {
		<-ss.Context().Done()
		return ss.Context().Err()
	})
	pcm := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	var res result
	done := make(chan struct{})
	go func() {
		Yandex(ctx, Options{Key: "k" + "ey-test", Endpoint: addr}, pcm, res.events())
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop on cancel")
	}
	if len(res.finals) != 0 || len(res.fails) != 0 {
		t.Fatalf("finals=%v fails=%v", res.finals, res.fails)
	}
}

func TestYandexFinalOnlyAfterCloseSend(t *testing.T) {
	_, addr := startFakeYandex(t, func(ss grpc.ServerStream, f *fakeYandex) error {
		if err := ss.SendMsg(respFinal(0, "раз")); err != nil {
			return err
		}
		drain(ss, f) // returns after the client's CloseSend
		return ss.SendMsg(respFinal(1, "два"))
	})
	pcm := make(chan []byte, 1)
	pcm <- make([]byte, 10)
	var res result
	done := make(chan struct{})
	go func() {
		Yandex(context.Background(), Options{Lang: "ru", Key: "k" + "ey-test", Endpoint: addr}, pcm, res.events())
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("finished before pcm was closed")
	case <-time.After(300 * time.Millisecond):
	}
	close(pcm)
	<-done
	if len(res.finals) != 1 || res.finals[0] != "раз два" || len(res.fails) != 0 {
		t.Fatalf("finals=%q fails=%v", res.finals, res.fails)
	}
}
