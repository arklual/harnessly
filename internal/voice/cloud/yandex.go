package cloud

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	yandexAddr      = "stt.api.cloud.yandex.net:443"
	yandexMethod    = "/speechkit.stt.v3.Recognizer/RecognizeStreaming"
	yandexMaxChunk  = 32 << 10
	yandexDrainWait = 10 * time.Second
)

type yandexFinal struct {
	idx  int64
	text string
}

// Yandex recognizes pcm with SpeechKit STT v3 over one bidirectional gRPC stream. Hints are not sent: v3 streaming
// has no vocabulary support, the runtime applies the dictionary to the text afterwards.
func Yandex(ctx context.Context, opts Options, pcm <-chan []byte, ev Events) {
	addr := yandexAddr
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if opts.Endpoint != "" {
		addr = opts.Endpoint
		creds = insecure.NewCredentials()
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.ForceCodecV2(rawCodec{})))
	if err != nil {
		ev.Fail(yandexMessage(err, opts))
		return
	}
	defer conn.Close()

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	md := metadata.Pairs("authorization", "Api-Key "+opts.Key)
	if opts.Folder != "" {
		md.Set("x-folder-id", opts.Folder)
	}
	sctx = metadata.NewOutgoingContext(sctx, md)

	stream, err := conn.NewStream(sctx, &grpc.StreamDesc{StreamName: "RecognizeStreaming", ClientStreams: true, ServerStreams: true}, yandexMethod)
	if err != nil {
		yandexFail(ctx, ev, err, opts)
		return
	}
	if err := stream.SendMsg(encodeSessionOptions(yandexLangs(opts.Lang))); err != nil {
		// A send error is io.EOF when the server closed the stream; the real status comes from the receive side.
		if errors.Is(err, io.EOF) {
			var tmp []byte
			err = stream.RecvMsg(&tmp)
		}
		yandexFail(ctx, ev, err, opts)
		return
	}
	if ev.Ready != nil {
		ev.Ready()
	}

	var timedOut atomic.Bool
	go func() {
		for {
			select {
			case <-sctx.Done():
				return
			case b, ok := <-pcm:
				if !ok {
					_ = stream.CloseSend()
					t := time.AfterFunc(yandexDrainWait, func() { timedOut.Store(true); cancel() })
					go func() { <-sctx.Done(); t.Stop() }()
					return
				}
				for len(b) > 0 {
					n := min(len(b), yandexMaxChunk)
					if stream.SendMsg(encodeChunk(b[:n])) != nil {
						return // the receive side reports why
					}
					b = b[n:]
				}
			}
		}
	}()

	var finals []yandexFinal
	var partial, lastShown string
	join := func(withPartial bool) string {
		parts := make([]string, 0, len(finals)+1)
		for _, f := range finals {
			parts = append(parts, f.text)
		}
		if withPartial {
			parts = append(parts, partial)
		}
		var kept []string
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, " ")
	}
	show := func() {
		if t := join(true); t != lastShown {
			lastShown = t
			if ev.Partial != nil {
				ev.Partial(t)
			}
		}
	}

	for {
		var msg []byte
		if err := stream.RecvMsg(&msg); err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, io.EOF) || (timedOut.Load() && status.Code(err) == codes.Canceled) {
				ev.Final(join(true))
				return
			}
			yandexFail(ctx, ev, err, opts)
			return
		}
		r, err := decodeResponse(msg)
		if err != nil {
			continue
		}
		switch {
		case r.Partial != nil:
			partial = *r.Partial
			show()
		case r.Final != nil:
			finals = append(finals, yandexFinal{r.FinalIndex, *r.Final})
			partial = ""
			show()
		case r.Refined != nil:
			for i := range finals {
				if finals[i].idx == r.RefineIdx && strings.TrimSpace(*r.Refined) != "" {
					finals[i].text = *r.Refined
				}
			}
			show()
		}
	}
}

func yandexFail(ctx context.Context, ev Events, err error, opts Options) {
	if ctx.Err() != nil {
		return
	}
	ev.Fail(yandexMessage(err, opts))
}

func yandexLangs(lang string) []string {
	switch lang {
	case "ru":
		return []string{"ru-RU"}
	case "en":
		return []string{"en-US"}
	default:
		return []string{"ru-RU", "en-US"}
	}
}

func yandexMessage(err error, opts Options) string {
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		if opts.Folder != "" {
			return "Яндекс SpeechKit не принял ключ или у него нет прав на распознавание. Проверьте ключ и идентификатор каталога в настройках."
		}
		return "Яндекс SpeechKit не принял ключ или у него нет прав на распознавание. Проверьте ключ в настройках."
	case codes.ResourceExhausted:
		return "Достигнут лимит Яндекс SpeechKit. Попробуйте позже или проверьте квоты в консоли Яндекса."
	case codes.Unavailable, codes.DeadlineExceeded:
		return "Не удалось связаться с Яндекс SpeechKit. Проверьте соединение и попробуйте ещё раз."
	default:
		return "Яндекс SpeechKit не смог распознать речь. Попробуйте ещё раз."
	}
}
