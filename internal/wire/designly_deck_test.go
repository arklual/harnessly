package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

// A new deck's create body: the deck's settings and the uploads of the first message are checked by Go and by the
// schema alike, and both stay optional for a page that does not know decks.
func TestDesignlyCreateCarriesADeck(t *testing.T) {
	sc := mustSchema(t)
	def := sc.defs["DesignlyCreate"].(map[string]any)
	twelve := 12
	good := DesignlyCreate{Title: "Acme onboarding", Brief: "Ten minutes on the new tools", Kind: DesignSlides, Provider: ProviderClaude,
		Deck: &DesignlyDeck{Minutes: 15, Slides: &twelve, Tone: "biz", Script: true,
			Style: &DesignlyDeckStyle{Source: "acme-template.pptx", Vars: map[string]string{"--deck-acc": "#ffdd2d"}, Layouts: 9}},
		AttachmentIDs: []string{"att-1", "att-2"}}
	open := good
	openDeck := DesignlyDeck{Tone: "talk", Script: true}
	open.Deck = &openDeck
	plain := DesignlyCreate{Title: "Acme site", Brief: "Landing", Kind: DesignSite, Provider: ProviderClaude}
	for name, c := range map[string]DesignlyCreate{"deck": good, "open deck": open, "plain": plain} {
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, _ := json.Marshal(c)
		v, err := decodeNumbers(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := sc.check(def, v, "$"); err != nil {
			t.Fatalf("%s: schema: %v", name, err)
		}
		var back DesignlyCreate
		if err := decodeStrict(raw, &back); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if name == "plain" && (strings.Contains(string(raw), "deck") || strings.Contains(string(raw), "attachmentIds")) {
			t.Fatalf("an old body grew fields: %s", raw)
		}
	}
	one, fifty := 1, 50
	bad := map[string]func(c *DesignlyCreate){
		"deck on a site":   func(c *DesignlyCreate) { c.Kind = DesignSite },
		"too few slides":   func(c *DesignlyCreate) { c.Deck.Slides = &one },
		"too many slides":  func(c *DesignlyCreate) { c.Deck.Slides = &fifty },
		"too long a talk":  func(c *DesignlyCreate) { c.Deck.Minutes = 181 },
		"negative minutes": func(c *DesignlyCreate) { c.Deck.Minutes = -1 },
		"unknown tone":     func(c *DesignlyCreate) { c.Deck.Tone = "loud" },
		"foreign variable": func(c *DesignlyCreate) { c.Deck.Style.Vars = map[string]string{"color": "red"} },
		"empty attachment": func(c *DesignlyCreate) { c.AttachmentIDs = []string{""} },
		"template unnamed": func(c *DesignlyCreate) { c.Deck.Style.Source = "" },
		"negative layouts": func(c *DesignlyCreate) { c.Deck.Style.Layouts = -1 },
		"too many uploads": func(c *DesignlyCreate) { c.AttachmentIDs = make([]string, DesignAttachmentsMax+1) },
	}
	for name, spoil := range bad {
		c := good
		deck, style := *good.Deck, *good.Deck.Style
		deck.Style = &style
		c.Deck = &deck
		spoil(&c)
		if c.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
