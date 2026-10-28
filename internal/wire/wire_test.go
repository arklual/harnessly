package wire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func mustSchema(t *testing.T) *schema {
	t.Helper()
	sc, err := loadSchema(filepath.Join("schema", "frames.json"))
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func fixtures(t *testing.T) map[string][]byte {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(filepath.Base(p), ".json")] = b
	}
	return out
}

// roundTrip decodes a fixture through the Go types, checks it and encodes it again.
func roundTrip(b []byte) ([]byte, error) {
	var head struct {
		F    FrameKind `json:"f"`
		Op   Op        `json:"op"`
		HTTP string    `json:"http"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, err
	}
	switch {
	case slices.Contains(ClientFrameKinds, head.F):
		f, err := DecodeClientFrame(b)
		if err != nil {
			return nil, err
		}
		return json.Marshal(f)
	case head.F != "":
		f, err := DecodeServerFrame(b)
		if err != nil {
			return nil, err
		}
		return json.Marshal(f)
	case head.Op != "":
		var x struct {
			Op     Op              `json:"op"`
			Body   json.RawMessage `json:"body"`
			Result CmdResult       `json:"result"`
		}
		if err := decodeStrict(b, &x); err != nil {
			return nil, err
		}
		c, err := DecodeCommand(x.Op, x.Body)
		if err != nil {
			return nil, err
		}
		if err := x.Result.Validate(); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"op": x.Op, "body": c, "result": x.Result})
	default:
		var x struct {
			HTTP     string          `json:"http"`
			Response json.RawMessage `json:"response"`
		}
		if err := decodeStrict(b, &x); err != nil {
			return nil, err
		}
		resp, err := decodePage(x.HTTP, x.Response)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"http": x.HTTP, "response": resp})
	}
}

// httpKinds is the answers of the v2 HTTP pages a fixture can hold.
var httpKinds = []string{"hello", "turns", "entries", "output", "upload", "dirs", "sessions", "terminals", "terminal", "voice", "designlyCreate", "designlyLibrary", "designlySystems", "designlyPin", "files", "file", "tasks", "legacy", "open", "openRefs", "messageStatus", "context", "search", "digest", "machineStats", "machineStop", "apps", "agentSettings", "runtimeSettings", "rewindPlan", "handoffDraft", "profiles", "profileKey", "profileTest", "login", "loginOK"}

func decodePage(kind string, b []byte) (interface{ Validate() error }, error) {
	var v interface{ Validate() error }
	var head struct {
		R ResultKind `json:"r"`
	}
	_ = json.Unmarshal(b, &head)
	switch {
	case head.R != "":
		var r CmdResult
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		if r.R != ResultRefused {
			return nil, fmt.Errorf("page answered %s", r.R)
		}
		return r, r.Validate()
	}
	switch kind {
	case "hello":
		v = &Hello{}
	case "turns":
		v = &TurnsPage{}
	case "entries":
		v = &EntriesPage{}
	case "output":
		v = &OutputPage{}
	case "upload":
		v = &AttachmentRef{}
	case "dirs":
		v = &Dirs{}
	case "sessions":
		v = &AgentSessions{}
	case "terminals":
		v = &Terminals{}
	case "terminal":
		v = &Terminal{}
	case "voice":
		v = &Transcript{}
	case "designlyCreate":
		v = &DesignlyCreated{}
	case "designlyLibrary":
		v = &DesignLibrary{}
	case "designlySystems":
		v = &DesignSystems{}
	case "designlyPin":
		v = &DesignlyPin{}
	case "tasks":
		v = &Tasks{}
	case "files":
		v = &DirListing{}
	case "file":
		v = &FileContent{}
	case "legacy":
		v = &LegacyTab{}
	case "context":
		v = &ConvContext{}
	case "messageStatus":
		v = &MessageStatuses{}
	case "search":
		v = &MessageSearch{}
	case "digest":
		v = &ChatDigest{}
	case "machineStats":
		v = &MachineStats{}
	case "machineStop":
		v = &MachineStopped{}
	case "apps":
		v = &AppsFeed{}
	case "open":
		v = &OpenTarget{}
	case "openRefs":
		v = &OpenedRefs{}
	case "agentSettings":
		v = &AgentSettings{}
	case "profiles":
		v = &ProfileList{}
	case "profileKey":
		v = &ProfileKey{}
	case "profileTest":
		v = &ProfileTest{}
	case "login":
		v = &LoginStatus{}
	case "loginOK":
		v = &LoginOK{}
	case "runtimeSettings":
		v = &RuntimeSettings{}
	case "rewindPlan":
		v = &RewindPlan{}
	case "handoffDraft":
		v = &HandoffDraft{}
	default:
		return nil, &json.UnsupportedValueError{Str: kind}
	}
	if err := decodeStrict(b, v); err != nil {
		return nil, err
	}
	return v, v.Validate()
}

func sameJSON(t *testing.T, want, got []byte) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(a, b)
}

func TestFixturesMatchTheSchemaAndRoundTrip(t *testing.T) {
	sc := mustSchema(t)
	fx := fixtures(t)
	if len(fx) < 30 {
		t.Fatalf("%d fixtures, the contract asks for at least 30", len(fx))
	}
	for name, b := range fx {
		t.Run(name, func(t *testing.T) {
			v, err := decodeNumbers(b)
			if err != nil {
				t.Fatal(err)
			}
			if err := sc.check(sc.root, v, "$"); err != nil {
				t.Fatalf("schema: %v", err)
			}
			out, err := roundTrip(b)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !sameJSON(t, b, out) {
				t.Fatalf("round trip lost or added data:\nwant %s\n got %s", compact(b), out)
			}
		})
	}
}

func compact(b []byte) string {
	var v any
	_ = json.Unmarshal(b, &v)
	out, _ := json.Marshal(v)
	return string(out)
}

// TestFixturesCoverTheProtocol: every event, frame, command, result and refuse code has a fixture.
func TestFixturesCoverTheProtocol(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range fixtures(t) {
		var x struct {
			F      string                `json:"f"`
			Ch     string                `json:"ch"`
			Mode   string                `json:"mode"`
			E      struct{ Type string } `json:"e"`
			Op     string                `json:"op"`
			Result struct{ R, Code string }
			HTTP   string `json:"http"`
		}
		if err := json.Unmarshal(b, &x); err != nil {
			t.Fatal(err)
		}
		seen["f:"+x.F] = true
		seen["ev:"+x.E.Type] = true
		seen["op:"+x.Op] = true
		seen["r:"+x.Result.R] = true
		seen["code:"+x.Result.Code] = true
		seen["http:"+x.HTTP] = true
		if x.F == string(FrameSnapshot) {
			ch := x.Ch
			if strings.HasPrefix(ch, convPrefix) {
				ch = "conv"
			}
			seen["snapshot:"+ch+":"+x.Mode] = true
		}
	}
	var want []string
	for _, k := range append(slices.Clone(ClientFrameKinds), ServerFrameKinds...) {
		want = append(want, "f:"+string(k))
	}
	for _, list := range [][]EventType{MachineEventTypes, ConvsEventTypes, ConvEventTypes} {
		for _, e := range list {
			want = append(want, "ev:"+string(e))
		}
	}
	for _, o := range Ops {
		want = append(want, "op:"+string(o))
	}
	for _, r := range ResultKinds {
		want = append(want, "r:"+string(r))
	}
	for _, c := range RefuseCodes {
		want = append(want, "code:"+string(c))
	}
	for _, h := range httpKinds {
		want = append(want, "http:"+h)
	}
	want = append(want, "snapshot:machine:replace", "snapshot:convs:replace", "snapshot:conv:replace",
		"snapshot:conv:after")
	for _, w := range want {
		if !seen[w] {
			t.Errorf("no fixture for %s", w)
		}
	}
}

func strs[T ~string](xs []T) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return out
}

// TestClosedListsMatchTheSchema: every closed list of the Go types is the one of the schema, in its order, and
// every union of the schema has the kinds of its list. v2.test.ts checks the TS side against the same schema.
func TestClosedListsMatchTheSchema(t *testing.T) {
	sc := mustSchema(t)
	lists := map[string][]string{
		"Provider": strs(Providers), "Feature": strs(Features), "Reach": strs(Reaches),
		"HealthLevel": strs(HealthLevels), "HealthMetric": strs(HealthMetrics),
		"AgentInstallStatus": strs(AgentInstallStatuses), "Effort": strs(Efforts),
		"TitleSource": strs(TitleSources), "GoalStatus": strs(GoalStatuses), "AgentStateKind": strs(AgentStateKinds), "LaunchStep": strs(LaunchSteps),
		"LaunchFailure": strs(LaunchFailures), "GoneReason": strs(GoneReasons), "TurnStatus": strs(TurnStatuses),
		"TurnFailureCode": strs(TurnFailureCodes), "FileOp": strs(FileOps), "OutputKind": strs(OutputKinds), "OutputOp": strs(OutputOps), "EntryKind": strs(EntryKinds),
		"SubagentStatus": strs(SubagentStatuses), "NoticeLevel": strs(NoticeLevels), "PeerNoteKind": strs(PeerNoteKinds),
		"AttachmentKind": strs(AttachmentKinds), "RefKind": strs(RefKinds), "MessageMode": strs(MessageModes),
		"MessageKind": strs(MessageKinds), "MessageStateKind": strs(MessageStateKinds),
		"QueueReason": strs(QueueReasons), "MessageFailure": strs(MessageFailures),
		"PauseReason": strs(PauseReasons), "TerminalFailureKind": strs(TerminalFailureKinds), "GateKind": strs(GateKinds), "PermissionKind": strs(PermissionKinds),
		"PermissionScope": strs(PermissionScopes), "GateStateKind": strs(GateStateKinds),
		"GateCloseReason": strs(GateCloseReasons), "GateRefusal": strs(GateRefusals), "BackgroundKind": strs(BackgroundKinds),
		"BackgroundStatus": strs(BackgroundStatuses), "SnapshotMode": strs(SnapshotModes),
		"FrameErrorCode": strs(FrameErrorCodes), "ClientFrameKind": strs(ClientFrameKinds),
		"ServerFrameKind": strs(ServerFrameKinds), "MachineEventType": strs(MachineEventTypes),
		"ConvsEventType": strs(ConvsEventTypes), "ConvEventType": strs(ConvEventTypes),
		"CompactionPhase": strs(CompactionPhases), "Op": strs(Ops), "PromoteTo": strs(PromoteTos),
		"DesignKind": strs(DesignKinds), "DesignDeckTone": strs(DesignDeckTones), "DesignRole": strs(DesignRoles), "FileKind": strs(FileKinds), "AppID": strs(AppIDs), "OpenTab": strs(OpenTabs), "AppState": strs(AppStates), "AppReason": strs(AppReasons), "AppSectionID": strs(AppSectionIDs), "AppFlag": strs(AppFlags), "AppTone": strs(AppTones), "AppRole": strs(AppRoles), "AppPipeline": strs(AppPipelines), "AgentOptionKey": strs(AgentOptionKeys), "AgentMcpKind": strs(AgentMcpKinds), "AgentAccountMethod": strs(AgentAccountMethods), "DiskPartID": strs(DiskPartIDs), "MachineProcKind": strs(MachineProcKinds), "MachineStopReason": strs(MachineStopReasons), "TaskStatus": strs(TaskStatuses), "ResultKind": strs(ResultKinds), "RefuseCode": strs(RefuseCodes), "RetryableRefuseCode": strs(RetryableRefuseCodes), "HttpKind": httpKinds, "TerminalStopBy": strs(TerminalStopBys), "IdleStop": strs(IdleStops), "FileChange": strs(FileChanges), "OriginKind": strs(OriginKinds), "OriginVia": strs(OriginVias), "HandoffMode": strs(HandoffModes), "DraftMode": strs(DraftModes), "OnOff": strs(OnOffs), "LimitWindow": strs(LimitWindows), "LimitAction": strs(LimitActions), "AutoContinueWhy": strs(AutoContinueWhys), "ProfileVendor": strs(ProfileVendors), "ProfileTestReason": strs(ProfileTestReasons), "LoginMode": strs(LoginModes), "LoginState": strs(LoginStates), "MessageRole": strs(MessageRoles), "LoginInput": strs(LoginInputs), "LoginReason": strs(LoginReasons), "MessageStatusKind": strs(MessageStatusKinds), "HandedWhere": strs(HandedWheres),
	}
	for name, def := range sc.defs {
		if _, isEnum := def.(map[string]any)["enum"]; isEnum {
			if _, ok := lists[name]; !ok {
				t.Errorf("schema list %s has no Go list", name)
			}
		}
	}
	for name, got := range lists {
		if want := sc.enum(name); !slices.Equal(got, want) {
			t.Errorf("%s:\n  go     %v\n  schema %v", name, got, want)
		}
	}
	unions := []struct{ def, disc, list string }{
		{"AgentState", "k", "AgentStateKind"}, {"MessageState", "k", "MessageStateKind"},
		{"GateState", "k", "GateStateKind"}, {"Entry", "t", "EntryKind"}, {"ClientFrame", "f", "ClientFrameKind"},
		{"ServerFrame", "f", "ServerFrameKind"}, {"MachineEvent", "type", "MachineEventType"},
		{"ConvsEvent", "type", "ConvsEventType"}, {"ConvEvent", "type", "ConvEventType"},
		{"Command", "op", "Op"}, {"CmdExchange", "op", "Op"}, {"HttpExchange", "http", "HttpKind"},
	}
	for _, u := range unions {
		if got, want := sc.kinds(u.def, u.disc), sc.enum(u.list); !slices.Equal(got, want) {
			t.Errorf("schema union %s.%s %v, list %s %v", u.def, u.disc, got, u.list, want)
		}
	}
	tables := map[string][]string{
		"AgentStateKind": keys(agentStateFields), "MessageStateKind": keys(messageStateFields),
		"GateStateKind": keys(gateStateFields), "EntryKind": keys(entryFields), "ResultKind": keys(cmdResultFields),
		"ClientFrameKind": keys(clientFrames), "ServerFrameKind": keys(serverFrames), "Op": keys(commands),
		"EventType": keys(events),
	}
	lists["EventType"] = slices.Concat(lists["MachineEventType"], lists["ConvsEventType"], lists["ConvEventType"])
	for name, got := range tables {
		want := slices.Clone(lists[name])
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("codec table of %s %v, list %v", name, got, want)
		}
	}
}

func keys[K ~string, V any](m map[K]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	slices.Sort(out)
	return out
}

func TestDecodersRefuseWhatTheProtocolDoesNotHave(t *testing.T) {
	cases := map[string]func() error{
		"unknown server frame": func() error { _, err := DecodeServerFrame([]byte(`{"f":"evt","t":1}`)); return err },
		"client frame as server": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"ping","t":1}`))
			return err
		},
		"unknown event": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"ev","ch":"convs","seq":1,"e":{"type":"conversation_moved","id":"tab-a"}}`))
			return err
		},
		"event on the wrong channel": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"ev","ch":"machine","seq":1,"e":{"type":"conversation_removed","id":"tab-a"}}`))
			return err
		},
		"unknown field": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"pong","t":1,"x":2}`))
			return err
		},
		"field of another kind": func() error {
			var s AgentState
			return json.Unmarshal([]byte(`{"k":"ready","since":1,"turnId":null}`), &s)
		},
		"missing field": func() error {
			var s AgentState
			return json.Unmarshal([]byte(`{"k":"working","since":1,"turnId":null}`), &s)
		},
		"unknown state": func() error {
			var s MessageState
			return json.Unmarshal([]byte(`{"k":"sent"}`), &s)
		},
		"client-only message state": func() error {
			var s MessageState
			return json.Unmarshal([]byte(`{"k":"submitting"}`), &s)
		},
		"unknown refuse code": func() error {
			var r CmdResult
			if err := json.Unmarshal([]byte(`{"r":"refused","code":"busy"}`), &r); err != nil {
				return err
			}
			return r.Validate()
		},
		"unknown op": func() error { _, err := DecodeCommand("send-now", []byte(`{"opId":"a"}`)); return err },
		"send with another opId": func() error {
			_, err := DecodeCommand(OpSend, []byte(`{"opId":"a","clientId":"b","conversationId":"tab-a","text":"","attachmentIds":[],"mode":"queue","kind":"prompt"}`))
			return err
		},
		"answer with both": func() error {
			_, err := DecodeCommand(OpAnswer, []byte(`{"opId":"a","conversationId":"tab-a","questionId":"q","answers":[{"i":0,"choices":[1]}],"chatAbout":true}`))
			return err
		},
		"snapshot with another channel's data": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"snapshot","ch":"convs","seq":0,"mode":"replace","data":{"machine":{}}}`))
			return err
		},
		"afterSeq without an epoch": func() error {
			_, err := DecodeClientFrame([]byte(`{"f":"sub","ch":"convs","afterSeq":3}`))
			return err
		},
		"a waiting state without its term": func() error {
			var s MessageState
			if err := json.Unmarshal([]byte(`{"k":"typing","wait":null}`), &s); err != nil {
				return err
			}
			return s.Validate()
		},
		"a random entry id": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"ev","ch":"conv:tab-a","seq":1,"e":{"type":"entry_replace","oldId":"9f2c","entry":{"t":"interrupt","id":"x:1#0","turnId":null,"at":1,"rev":1}}}`))
			return err
		},
		"a live entry as a turn": func() error {
			var s AgentState
			if err := json.Unmarshal([]byte(`{"k":"working","since":1,"turnId":"live:x:1#0","compacting":false}`), &s); err != nil {
				return err
			}
			return s.Validate()
		},
		"an answer with choices and text": func() error {
			_, err := DecodeCommand(OpAnswer, []byte(`{"opId":"a","conversationId":"tab-a","questionId":"q:1","answers":[{"i":0,"choices":[1],"text":"x"}]}`))
			return err
		},
		"an answer with neither": func() error {
			_, err := DecodeCommand(OpAnswer, []byte(`{"opId":"a","conversationId":"tab-a","questionId":"q:1","answers":[{"i":0,"choices":[]}]}`))
			return err
		},
		"a permission id for a question": func() error {
			_, err := DecodeCommand(OpAnswer, []byte(`{"opId":"a","conversationId":"tab-a","questionId":"p:1","chatAbout":true}`))
			return err
		},
		"a project at a relative path": func() error {
			_, err := DecodeCommand(OpProjectCreate, []byte(`{"opId":"p","path":"work/app"}`))
			return err
		},
		"a project event without its project": func() error {
			_, err := DecodeServerFrame([]byte(`{"f":"ev","ch":"convs","seq":1,"e":{"type":"project_upsert","project":{"id":"","machineId":"m","path":"/w","name":"w","git":false,"createdAt":1}}}`))
			return err
		},
		"bad channel": func() error { _, err := DecodeClientFrame([]byte(`{"f":"sub","ch":"conv:pane-1"}`)); return err },
	}
	for name, run := range cases {
		if run() == nil {
			t.Errorf("%s: decoded without an error", name)
		}
	}
}

func TestEncodersRefuseAnUnknownKind(t *testing.T) {
	if _, err := json.Marshal(AgentState{}); err == nil {
		t.Error("an AgentState without a kind encoded")
	}
	if _, err := json.Marshal(SnapshotFrame{Ch: ChannelConvs, Mode: SnapshotReplace}); err == nil {
		t.Error("a snapshot without data encoded")
	}
}

func TestChannels(t *testing.T) {
	for _, c := range []Channel{"machine", "convs", "conv:tab-k3Jx9QmZ"} {
		if !c.Valid() {
			t.Errorf("%q is not valid", c)
		}
	}
	for _, c := range []Channel{"", "conv:", "conv:tab-", "conv:pane-1", "conv:tab-a b", "machines"} {
		if c.Valid() {
			t.Errorf("%q is valid", c)
		}
	}
	if id, ok := ConvChannel("tab-x").ConvID(); !ok || id != "tab-x" {
		t.Errorf("ConvChannel(tab-x).ConvID() = %q, %v", id, ok)
	}
}

func TestEntryIDs(t *testing.T) {
	claude := ClaudeEntryID("3F1D2A9E-7C41-4B8E-9A51-0D6C2B7E8F10", 2)
	if claude != "c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f10#2" || !ValidTurnID(claude) {
		t.Errorf("ClaudeEntryID = %q", claude)
	}
	codex := OffsetEntryID(18234, 0)
	if codex != "x:18234#0" || !ValidTurnID(codex) {
		t.Errorf("OffsetEntryID = %q", codex)
	}
	if live := LiveEntryID(codex); !ValidEntryID(live) || ValidTurnID(live) {
		t.Errorf("LiveEntryID = %q: an entry id, not a turn id", live)
	}
	for _, bad := range []string{"", "c:#0", "c:abc", "x:12", "x:1a#0", "c:ABC#0", "live:live:x:1#0", "9f2c1b"} {
		if ValidEntryID(bad) {
			t.Errorf("%q is a valid entry id", bad)
		}
	}
}
