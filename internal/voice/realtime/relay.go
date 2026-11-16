package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// The sound of a call over the machine. Some networks carry no UDP to the voice's media servers, or lose
// its answers on the way back (a VPN client that tunnels TCP well and UDP badly): the page's own peer connection then
// never connects. Over the machine the machine is the peer: it makes the call with its own offer, and the page sends
// and gets the Opus frames over one WebSocket on the page's usual HTTPS way to the machine. Nothing is transcoded: a
// frame the page encoded goes out as one RTP packet, a packet that came in goes to the page as it is.
//
// The socket carries binary messages, one Opus packet each (20 ms, 48 kHz), both ways, and text messages: the page's
// go to the call's data channel (oai-events, the key route's cancels), the data channel's come to the page, and the
// machine's own say how the sound is: {"type":"relay.state","state":"connected"|"failed"|"closed"}.

const (
	// relayFrame is the length of one Opus frame the page sends.
	relayFrame = 20 * time.Millisecond
	// relayOutQueue frames wait for a slow page; older ones are dropped so the sound never lags behind.
	relayOutQueue = 64
	relayMaxFrame = 4 << 10
)

// Relay states told to the page.
const (
	RelayConnecting = "connecting"
	RelayConnected  = "connected"
	RelayFailed     = "failed"
	RelayClosed     = "closed"
)

type relayOut struct {
	kind websocket.MessageType
	data []byte
}

// relay is the machine's peer connection of one call.
type relay struct {
	pc     *webrtc.PeerConnection
	mic    *webrtc.TrackLocalStaticSample
	events *webrtc.DataChannel

	// note tells the runtime's journal how the machine's connection goes (Config.Relayed); never the sound.
	note  func(level, text string)
	start time.Time
	// fromPageN, toPageN count the frames each way, for the journal's last line.
	fromPageN, toPageN atomic.Int64

	mu    sync.Mutex
	state string
	out   chan relayOut // to the page that holds the socket now; nil while none does
	drop  func()        // ends the socket of that page
	done  bool
}

// newRelay makes the machine's peer: a send-and-receive Opus track and the oai-events data channel, as the page's own
// peer connection would have.
func newRelay(note func(level, text string)) (*relay, error) {
	if note == nil {
		note = func(string, string) {}
	}
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
			SDPFmtpLine: "minptime=10;useinbandfec=1"},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	var se webrtc.SettingEngine
	se.SetInterfaceFilter(outward)
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	r := &relay{pc: pc, state: RelayConnecting, note: note, start: time.Now()}
	fail := func(err error) (*relay, error) {
		_ = pc.Close()
		return nil, err
	}
	r.mic, err = webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "harnessly")
	if err != nil {
		return fail(err)
	}
	tr, err := pc.AddTransceiverFromTrack(r.mic, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv})
	if err != nil {
		return fail(err)
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := tr.Sender().Read(buf); err != nil {
				return
			}
		}
	}()
	ordered := true
	r.events, err = pc.CreateDataChannel("oai-events", &webrtc.DataChannelInit{Ordered: &ordered})
	if err != nil {
		return fail(err)
	}
	r.events.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			r.send(websocket.MessageText, msg.Data)
		}
	})
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			pkt, _, err := t.ReadRTP()
			if err != nil {
				return
			}
			if len(pkt.Payload) > 0 {
				r.send(websocket.MessageBinary, pkt.Payload)
			}
		}
	})
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		level, text := "info", fmt.Sprintf("ice=%s after %s", s, r.since())
		switch s {
		case webrtc.ICEConnectionStateChecking:
			// Over a healthy network the checks end in a fraction of a second; one still at it says why in the journal.
			time.AfterFunc(relayChecksSlow, func() {
				if pc.ICEConnectionState() == webrtc.ICEConnectionStateChecking {
					r.note("warn", fmt.Sprintf("ice still checking after %s: %s", r.since(), checksOf(pc.GetStats())))
				}
			})
		case webrtc.ICEConnectionStateConnected:
			if p, err := pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair(); err == nil && p != nil {
				text += fmt.Sprintf(" pair=%s %s → %s %s", p.Local.Protocol, p.Local.Typ, p.Remote.Protocol, p.Remote.Typ)
			}
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateDisconnected:
			level = "warn"
			text += ": " + checksOf(pc.GetStats())
		}
		r.note(level, text)
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		level := "info"
		if s == webrtc.PeerConnectionStateFailed {
			level = "warn"
		}
		r.note(level, fmt.Sprintf("conn=%s after %s", s, r.since()))
		switch s {
		case webrtc.PeerConnectionStateConnected:
			r.setState(RelayConnected)
		case webrtc.PeerConnectionStateFailed:
			r.setState(RelayFailed)
		case webrtc.PeerConnectionStateClosed:
			r.setState(RelayClosed)
		}
	})
	return r, nil
}

// outward leaves out the interfaces no packet to the voice's media servers leaves by: the bridges of containers and
// virtual machines and their ends. A machine with a dozen of them pairs each with every server candidate, and the
// checks multiply for nothing.
func outward(name string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "lxc", "lxd"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

// relayChecksSlow is how long the machine's connectivity checks may go before the journal hears how they go.
const relayChecksSlow = 4 * time.Second

// checksOf tells how the connectivity checks go: the pairs by state, the checks sent and answered both ways and the
// machine's own candidates by network and address (the machine's addresses, never the page's or the person's).
func checksOf(rep webrtc.StatsReport) string {
	states := map[string]int{}
	var sent, answered, asked uint64
	var local []string
	remote := 0
	for _, st := range rep {
		switch v := st.(type) {
		case webrtc.ICECandidatePairStats:
			states[string(v.State)]++
			sent += v.RequestsSent
			answered += v.ResponsesReceived
			asked += v.RequestsReceived
		case webrtc.ICECandidateStats:
			if v.Type == webrtc.StatsTypeLocalCandidate {
				local = append(local, fmt.Sprintf("%s %s %s", v.Protocol, v.CandidateType, v.IP))
			} else if v.Type == webrtc.StatsTypeRemoteCandidate {
				remote++
			}
		}
	}
	var pairs []string
	for k, n := range states {
		pairs = append(pairs, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(pairs)
	sort.Strings(local)
	return fmt.Sprintf("pairs [%s], checks sent %d answered %d, checks from the server %d, local [%s], remote %d",
		strings.Join(pairs, ", "), sent, answered, asked, strings.Join(local, ", "), remote)
}

func (r *relay) since() time.Duration { return time.Since(r.start).Round(10 * time.Millisecond) }

// offer is the machine's offer with its candidates gathered (the voice's server takes no trickled ones).
func (r *relay) offer(ctx context.Context) (string, error) {
	o, err := r.pc.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	gathered := webrtc.GatheringCompletePromise(r.pc)
	if err := r.pc.SetLocalDescription(o); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(3 * time.Second):
		// What is gathered by then is enough: the host candidates come first.
	}
	return r.pc.LocalDescription().SDP, nil
}

func (r *relay) answer(sdp string) error {
	return r.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp})
}

func stateFrame(s string) []byte {
	raw, _ := json.Marshal(map[string]string{"type": "relay.state", "state": s})
	return raw
}

func (r *relay) setState(s string) {
	r.mu.Lock()
	if r.state == s || r.state == RelayClosed {
		r.mu.Unlock()
		return
	}
	r.state = s
	r.mu.Unlock()
	r.send(websocket.MessageText, stateFrame(s))
}

// send queues a message for the page; a page that does not keep up loses the oldest sound, never the newest.
func (r *relay) send(kind websocket.MessageType, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.out == nil {
		return
	}
	m := relayOut{kind: kind, data: append([]byte(nil), data...)}
	for {
		select {
		case r.out <- m:
			return
		default:
		}
		select {
		case <-r.out:
		default:
		}
	}
}

// attach makes the page's socket the one the sound goes to; a page that comes again takes it over.
func (r *relay) attach(drop func()) (<-chan relayOut, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil, RelayClosed, false
	}
	if r.drop != nil {
		r.drop()
	}
	r.out = make(chan relayOut, relayOutQueue)
	r.drop = drop
	r.note("info", fmt.Sprintf("page joined after %s, state=%s", r.since(), r.state))
	return r.out, r.state, true
}

func (r *relay) detach(out <-chan relayOut) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.out == out {
		r.out, r.drop = nil, nil
	}
}

// fromPage takes one message of the page: an Opus frame for the call, or an event for its data channel.
func (r *relay) fromPage(kind websocket.MessageType, data []byte) {
	if kind == websocket.MessageBinary {
		if len(data) > 0 && len(data) <= relayMaxFrame {
			r.fromPageN.Add(1)
			_ = r.mic.WriteSample(media.Sample{Data: data, Duration: relayFrame})
		}
		return
	}
	if r.events.ReadyState() == webrtc.DataChannelStateOpen {
		_ = r.events.SendText(string(data))
	}
}

func (r *relay) close() {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return
	}
	r.done = true
	drop := r.drop
	state := r.state
	r.mu.Unlock()
	r.note("info", fmt.Sprintf("closed after %s, state=%s, frames from page %d, to page %d",
		r.since(), state, r.fromPageN.Load(), r.toPageN.Load()))
	r.setState(RelayClosed)
	if drop != nil {
		drop()
	}
	_ = r.pc.Close()
}

// serveAudio joins the page to the call's sound over the machine.
func serveAudio(w http.ResponseWriter, r *http.Request, c *Call) {
	rl := c.relay
	if rl == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": CodeNoCall, "detail": "this call's sound is not over the machine"})
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The guard has checked the origin: through the hub the Host is not the page's.
		InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	out, state, ok := rl.attach(cancel)
	if !ok {
		_ = ws.Close(websocket.StatusNormalClosure, RelayClosed)
		return
	}
	defer rl.detach(out)
	go func() {
		defer cancel()
		_ = ws.Write(ctx, websocket.MessageText, stateFrame(state))
		for {
			select {
			case m := <-out:
				if err := ws.Write(ctx, m.kind, m.data); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			break
		}
		rl.fromPage(kind, data)
	}
	code := websocket.StatusNormalClosure
	if errors.Is(ctx.Err(), context.Canceled) && r.Context().Err() == nil {
		code = websocket.StatusGoingAway
	}
	_ = ws.Close(code, "")
}
