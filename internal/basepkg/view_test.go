package basepkg

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func lookWith(have map[string]bool) func(string) (string, error) {
	return func(b string) (string, error) {
		if have[b] {
			return "/usr/bin/" + b, nil
		}
		return "", errors.New("no")
	}
}

func line(v View, id string) ViewItem {
	for _, it := range v.Items {
		if it.ID == id {
			return it
		}
	}
	return ViewItem{}
}

func TestViewWithoutAServiceGivesTheCommand(t *testing.T) {
	v := MakeView(Look{LookPath: lookWith(allBut("ffmpeg", "jq")), Manager: Apt, Voice: Voice{Ready: true},
		Inbox: Inbox{Skip: map[string]bool{"jq": true}}})
	if v.Service || v.Done {
		t.Fatalf("%+v", v)
	}
	if l := line(v, "ffmpeg"); l.Status != Missing || !strings.HasPrefix(l.Command, "sudo apt-get install") {
		t.Fatalf("%+v", l)
	}
	if line(v, "jq").Status != Skipped || line(v, "git").Status != OK || line(v, VoiceID).Status != OK {
		t.Fatalf("%+v", v.Items)
	}
	if !strings.HasSuffix(v.Command, " ffmpeg") {
		t.Fatalf("skipped items are not in the command: %q", v.Command)
	}
}

func TestViewReadsTheServiceAndTheVoice(t *testing.T) {
	st := State{Hash: Hash(), Items: []ItemState{{ID: "ffmpeg", Status: NoAccess, Message: "нет доступа", Host: "deb.debian.org"}}}
	v := MakeView(Look{LookPath: lookWith(allBut("ffmpeg")), Manager: Apt, HasService: true, State: st,
		Inbox: Inbox{Skip: map[string]bool{}, Proxy: "http://p:3128"},
		Voice: Voice{Error: "Не удалось скачать модель распознавания: dial tcp: lookup huggingface.co: no such host"}})
	if l := line(v, "ffmpeg"); l.Status != NoAccess || l.Host != "deb.debian.org" || l.Command != "" {
		t.Fatalf("%+v", l)
	}
	if l := line(v, VoiceID); l.Status != NoAccess || l.Host != "huggingface.co" {
		t.Fatalf("%+v", l)
	}
	if v.Proxy != "http://p:3128" || v.Done {
		t.Fatalf("%+v", v)
	}
	// Everything in place: done.
	v = MakeView(Look{LookPath: lookWith(allBut()), Manager: Apt, HasService: true, State: st, Voice: Voice{Ready: true}})
	if !v.Done || line(v, "ffmpeg").Status != OK {
		t.Fatalf("%+v", v)
	}
}

func TestSkipProxyAndAskGoToTheInbox(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSkip(dir, "chrome", true); err != nil {
		t.Fatal(err)
	}
	if err := WriteSkip(dir, "nothing", true); err == nil {
		t.Fatal("an unknown item is not kept")
	}
	if err := WriteProxy(dir, "http://user:pw@p:1"); err == nil {
		t.Fatal("a login in the address is refused")
	}
	if err := WriteProxy(dir, "http://p:3128"); err != nil {
		t.Fatal(err)
	}
	if err := Ask(dir, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	in := ReadInbox(dir)
	if !in.Skip["chrome"] || in.Proxy != "http://p:3128" {
		t.Fatalf("%+v", in)
	}
	_ = WriteSkip(dir, "chrome", false)
	_ = WriteProxy(dir, "")
	if in := ReadInbox(dir); len(in.Skip) != 0 || in.Proxy != "" {
		t.Fatalf("%+v", in)
	}
}

// A missing optional tool (LibreOffice) is no problem of the first screen: the base package is done without it and
// the one-line command leaves it out.
func TestViewDoneWithoutTheOptionalTools(t *testing.T) {
	v := MakeView(Look{LookPath: lookWith(allBut("libreoffice")), Manager: Apt, Voice: Voice{Ready: true}})
	l := line(v, "libreoffice")
	if l.Status != Missing || !l.Optional || l.Command == "" {
		t.Fatalf("the optional tool is still listed: %+v", l)
	}
	if !v.Done || v.Command != "" {
		t.Fatalf("done %v, command %q", v.Done, v.Command)
	}
	// A required tool missing still stops it, and the optional one stays out of the command.
	v = MakeView(Look{LookPath: lookWith(allBut("libreoffice", "jq")), Manager: Apt, Voice: Voice{Ready: true}})
	if v.Done || !strings.HasSuffix(v.Command, " jq") || strings.Contains(v.Command, "libreoffice") {
		t.Fatalf("done %v, command %q", v.Done, v.Command)
	}
}
