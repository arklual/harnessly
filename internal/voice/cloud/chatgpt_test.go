package cloud

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatGPTRun recognizes pcm against the handler and gives what the events said.
func chatGPTRun(t *testing.T, ctx context.Context, opts Options, pcm [][]byte, h http.HandlerFunc) (ready bool, final *string, failure string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	opts.Endpoint = srv.URL
	ch := make(chan []byte, len(pcm))
	for _, b := range pcm {
		ch <- b
	}
	close(ch)
	ChatGPT(ctx, opts, ch, Events{
		Ready:   func() { ready = true },
		Partial: func(string) { t.Error("a partial text from an engine without a stream") },
		Final:   func(s string) { final = &s },
		Fail:    func(m string) { failure = m },
	})
	return ready, final, failure
}

// The recording goes up whole as a WAV, on the sign-in and under this client's own name.
func TestChatGPTSendsTheRecording(t *testing.T) {
	var got *http.Request
	var file []byte
	var language string
	ready, final, failure := chatGPTRun(t, context.Background(), Options{Key: "tok", Folder: "acc", Lang: "ru"},
		[][]byte{{1, 2}, {3, 4}}, func(w http.ResponseWriter, r *http.Request) {
			got = r
			f, h, err := r.FormFile("file")
			if err != nil {
				t.Errorf("file: %v", err)
				return
			}
			defer f.Close()
			if h.Header.Get("Content-Type") != "audio/wav" {
				t.Errorf("file type: %q", h.Header.Get("Content-Type"))
			}
			file, _ = io.ReadAll(f)
			language = r.FormValue("language")
			_, _ = w.Write([]byte(`{"text":" привет ","asset_ttl":"30d"}`))
		})
	if !ready || failure != "" || final == nil || *final != "привет" {
		t.Fatalf("ready %v, final %v, failure %q", ready, final, failure)
	}
	if got.Header.Get("Authorization") != "Bearer tok" || got.Header.Get("ChatGPT-Account-Id") != "acc" {
		t.Fatalf("sign-in: %v", got.Header)
	}
	if got.Header.Get("User-Agent") != chatGPTClient || got.Header.Get("originator") != "" {
		t.Fatalf("client's name: %v", got.Header)
	}
	if language != "ru" {
		t.Fatalf("language: %q", language)
	}
	if len(file) != 48 || string(file[:4]) != "RIFF" || string(file[44:]) != "\x01\x02\x03\x04" {
		t.Fatalf("file: %x", file)
	}
}

// With "auto" no language is named.
func TestChatGPTAutoLanguage(t *testing.T) {
	_, final, _ := chatGPTRun(t, context.Background(), Options{Key: "tok", Lang: "auto"}, [][]byte{{1, 2}},
		func(w http.ResponseWriter, r *http.Request) {
			if _, has := r.MultipartForm, r.FormValue("language") != ""; has {
				t.Error("a language was named")
			}
			if r.Header.Get("ChatGPT-Account-Id") != "" {
				t.Error("an account out of nowhere")
			}
			_, _ = w.Write([]byte(`{"text":""}`))
		})
	if final == nil || *final != "" {
		t.Fatalf("final %v", final)
	}
}

// A refusal says what to do in the person's words, and nothing of the server's answer or the token gets into them.
func TestChatGPTRefusals(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusUnauthorized:        "codex login",
		http.StatusForbidden:           "другую модель",
		http.StatusTooManyRequests:     "Лимит",
		http.StatusInternalServerError: "500",
	} {
		_, final, failure := chatGPTRun(t, context.Background(), Options{Key: "secret-token"}, [][]byte{{1, 2}},
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("server body secret-token"))
			})
		if final != nil || !strings.Contains(failure, want) || strings.Contains(failure, "secret-token") || strings.Contains(failure, "server body") {
			t.Fatalf("%d: final %v, failure %q", status, final, failure)
		}
	}
}

// An answer without text is a failure, not an empty dictation: the way may have changed.
func TestChatGPTAnswerWithoutText(t *testing.T) {
	for _, body := range []string{`{"detail":"x"}`, `<html>`} {
		_, final, failure := chatGPTRun(t, context.Background(), Options{Key: "tok"}, [][]byte{{1, 2}},
			func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		if final != nil || failure == "" {
			t.Fatalf("%q: final %v, failure %q", body, final, failure)
		}
	}
}

// Without a sign-in nothing is asked; an empty recording is not sent; a cancelled recognition reports nothing.
func TestChatGPTSendsNothing(t *testing.T) {
	never := func(http.ResponseWriter, *http.Request) { t.Error("a request") }
	if ready, final, failure := chatGPTRun(t, context.Background(), Options{}, [][]byte{{1, 2}}, never); ready || final != nil || !strings.Contains(failure, "codex login") {
		t.Fatalf("no sign-in: %v %v %q", ready, final, failure)
	}
	if _, final, failure := chatGPTRun(t, context.Background(), Options{Key: "tok"}, nil, never); final == nil || *final != "" || failure != "" {
		t.Fatalf("empty: %v %q", final, failure)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, final, failure := chatGPTRun(t, ctx, Options{Key: "tok"}, [][]byte{{1, 2}}, never); final != nil || failure != "" {
		t.Fatalf("cancelled: %v %q", final, failure)
	}
}

func TestChatGPTTooLong(t *testing.T) {
	_, final, failure := chatGPTRun(t, context.Background(), Options{Key: "tok"}, [][]byte{make([]byte, chatGPTHoldMax+1)},
		func(http.ResponseWriter, *http.Request) { t.Error("a request") })
	if final != nil || !strings.Contains(failure, "30 минут") {
		t.Fatalf("final %v, failure %q", final, failure)
	}
}
