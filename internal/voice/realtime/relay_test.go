package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// voicePeer stands for the voice's media server: it answers the machine's offer, sends a tone of frames and keeps
// what came to it.
type voicePeer struct {
	mu     sync.Mutex
	got    [][]byte
	events []string
	pc     *webrtc.PeerConnection
	track  *webrtc.TrackLocalStaticSample
}

func (v *voicePeer) answer(t *testing.T, offer string) string {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	v.pc = pc
	v.track, _ = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "voice")
	if _, err := pc.AddTrack(v.track); err != nil {
		t.Fatal(err)
	}
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			pkt, _, err := tr.ReadRTP()
			if err != nil {
				return
			}
			v.mu.Lock()
			v.got = append(v.got, append([]byte(nil), pkt.Payload...))
			v.mu.Unlock()
		}
	})
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			v.mu.Lock()
			v.events = append(v.events, string(m.Data))
			v.mu.Unlock()
			_ = dc.SendText(`{"type":"input_audio_buffer.speech_started"}`)
		})
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		t.Fatal(err)
	}
	a, err := pc.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(a); err != nil {
		t.Fatal(err)
	}
	<-done
	return pc.LocalDescription().SDP
}

func TestRelayCarriesTheSoundBothWays(t *testing.T) {
	v := &voicePeer{}
	voice := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/realtime/calls" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			SDP string `json:"sdp"`
		}
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Location", "/v1/realtime/calls/rtc_relay1")
		_, _ = io.WriteString(w, v.answer(t, body.SDP))
	}))
	t.Cleanup(voice.Close)
	s := service(t, voice, true)
	_ = s.SaveSettings(Settings{Subscription: true})
	if !s.State().Relay {
		t.Fatal("the state does not say the machine relays")
	}
	st, err := s.Start(context.Background(), CallInput{Relay: true, Mode: ModeAgent})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Relay || st.SDP != "" || st.ID != "rtc_relay1" {
		t.Fatalf("started %+v", st)
	}
	page := httptest.NewServer(s.Handler("/voice", func(http.ResponseWriter, *http.Request) bool { return true }))
	t.Cleanup(page.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(page.URL, "http")+"/voice/calls/rtc_relay1/audio", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()

	var (
		mu        sync.Mutex
		heard     [][]byte
		texts     []string
		connected = make(chan struct{})
		closed    = make(chan struct{})
	)
	go func() {
		defer close(closed)
		once := sync.Once{}
		for {
			kind, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			if kind == websocket.MessageBinary {
				heard = append(heard, data)
			} else {
				texts = append(texts, string(data))
				if strings.Contains(string(data), `"connected"`) {
					once.Do(func() { close(connected) })
				}
			}
			mu.Unlock()
		}
	}()
	select {
	case <-connected:
	case <-ctx.Done():
		t.Fatal("the machine's peer never connected")
	}
	said := []byte{0xfc, 0xff, 0xfe, 0x01, 0x02}
	tone := []byte{0xf8, 0x0a, 0x0b}
	until := func(what string, ok func() bool) {
		for !ok() {
			select {
			case <-ctx.Done():
				t.Fatal(what)
			case <-time.After(20 * time.Millisecond):
			}
			_ = ws.Write(ctx, websocket.MessageBinary, said)
			_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"response.cancel"}`))
			_ = v.track.WriteSample(media.Sample{Data: tone, Duration: 20 * time.Millisecond})
		}
	}
	until("the voice never heard the page", func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		return len(v.got) > 0 && bytes.Equal(v.got[0], said)
	})
	until("the page never heard the voice", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(heard) > 0 && bytes.Equal(heard[0], tone)
	})
	until("the data channel did not carry the events", func() bool {
		v.mu.Lock()
		got := strings.Join(v.events, " ")
		v.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(got, "response.cancel") && strings.Contains(strings.Join(texts, " "), "speech_started")
	})

	// A hung up call ends the page's socket.
	s.Hang(st.ID)
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("the socket outlived the call")
	}
}

func TestAudioOfACallWithoutRelayIsRefused(t *testing.T) {
	_, srv := newFake(t)
	s := service(t, srv, true)
	_ = s.SaveSettings(Settings{Subscription: true})
	st, err := s.Start(context.Background(), CallInput{SDP: "v=0 offer"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Hang(st.ID)
	page := httptest.NewServer(s.Handler("/voice", func(http.ResponseWriter, *http.Request) bool { return true }))
	t.Cleanup(page.Close)
	resp, err := http.Get(page.URL + "/voice/calls/" + st.ID + "/audio")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestRelayLeavesOutContainerBridges(t *testing.T) {
	for name, want := range map[string]bool{"ens3": true, "eth0": true, "wg0": true, "wt0": true,
		"docker0": false, "br-1a2b3c": false, "veth9f8e": false, "virbr0": false} {
		if got := outward(name); got != want {
			t.Errorf("outward(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestChecksOfTellsPairsAndCandidates(t *testing.T) {
	got := checksOf(webrtc.StatsReport{
		"p1": webrtc.ICECandidatePairStats{State: webrtc.StatsICECandidatePairStateInProgress, RequestsSent: 3},
		"p2": webrtc.ICECandidatePairStats{State: webrtc.StatsICECandidatePairStateFailed, RequestsSent: 5, ResponsesReceived: 0},
		"l1": webrtc.ICECandidateStats{Type: webrtc.StatsTypeLocalCandidate, Protocol: "udp", CandidateType: webrtc.ICECandidateTypeHost, IP: "192.0.2.10"},
		"r1": webrtc.ICECandidateStats{Type: webrtc.StatsTypeRemoteCandidate, Protocol: "udp", IP: "198.51.100.1"},
	})
	want := "pairs [failed 1, in-progress 1], checks sent 8 answered 0, checks from the server 0, local [udp host 192.0.2.10], remote 1"
	if got != want {
		t.Fatalf("checksOf:\n got %s\nwant %s", got, want)
	}
}
