package design

// The kits of Sinopia's home. KitCards reads every theme's manifest of every installed kit and checks each
// version's seal (a stat of every file of it, a hash of every file when the seal moved): on a machine with a few kits
// that is hundreds of milliseconds to seconds, and the home waited for it on every open. A version never changes once
// installed, so the cards are kept while the kits folder looks the same — its kits, their versions, their records and
// their seals — and are written beside the folder, so a runtime that just started answers from the last ones too.
// Whatever moves in the folder (an install, a removal, a seal written anew) reads the cards again; the files of a
// sealed version are also looked over again after kitCardsRecheck, in the background, while the kept cards answer.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// kitCardsRecheck is how long kept cards answer before the files under them are looked over again (in the background).
const kitCardsRecheck = 5 * time.Minute

type kitCardsEntry struct {
	sig   string
	at    time.Time
	cards []KitCard
}

var kitCardsKept = struct {
	sync.Mutex
	by      map[string]*kitCardsEntry
	reading map[string]bool
}{by: map[string]*kitCardsEntry{}, reading: map[string]bool{}}

// kitCardsNow is the clock (tests move it).
var kitCardsNow = time.Now

// kitsSignature is what the cards depend on that can change: the kits, their versions and records, each version's
// seal, and the mark that seals are required. A few stats per kit; the files inside a version are not read.
func kitsSignature(dir string) string {
	var b strings.Builder
	if st, err := os.Stat(filepath.Join(dir, designkit.SealRequired)); err == nil {
		b.WriteString("sealed;" + st.ModTime().String() + ";")
	}
	kits, err := os.ReadDir(dir)
	if err != nil {
		return "none"
	}
	for _, k := range kits {
		if !k.IsDir() || !designkit.ValidID(k.Name()) {
			continue
		}
		b.WriteString("k:" + k.Name() + ";")
		entries, err := os.ReadDir(filepath.Join(dir, k.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			b.WriteString(e.Name() + "@" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "/" +
				strconv.FormatInt(info.Size(), 10) + ";")
			if e.IsDir() && designkit.ValidVersion(e.Name()) {
				if st, err := os.Stat(filepath.Join(dir, k.Name(), e.Name(), designkit.SealFile)); err == nil {
					b.WriteString("s@" + strconv.FormatInt(st.ModTime().UnixNano(), 10) + "/" +
						strconv.FormatInt(st.Size(), 10) + ";")
				}
			}
		}
	}
	return b.String()
}

// kitCardsFile is where the cards of a kits folder are written: beside it, not in it (the folder is the kits').
func kitCardsFile(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(filepath.Dir(dir), ".kit-cards-"+hex.EncodeToString(sum[:6])+".json")
}

type kitCardsDisk struct {
	Sig   string    `json:"sig"`
	Cards []KitCard `json:"cards"`
}

func readKitCardsDisk(dir, sig string) ([]KitCard, bool) {
	raw, err := os.ReadFile(kitCardsFile(dir))
	if err != nil {
		return nil, false
	}
	var d kitCardsDisk
	if json.Unmarshal(raw, &d) != nil || d.Sig != sig || d.Cards == nil {
		return nil, false
	}
	return d.Cards, true
}

func writeKitCardsDisk(dir, sig string, cards []KitCard) {
	raw, err := json.Marshal(kitCardsDisk{Sig: sig, Cards: cards})
	if err != nil {
		return
	}
	file := kitCardsFile(dir)
	tmp := file + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, file)
	}
}

// readKitCards reads the cards anew and keeps them.
func readKitCards(dir, sig string) []KitCard {
	cards := KitCards(dir)
	kitCardsKept.Lock()
	kitCardsKept.by[dir] = &kitCardsEntry{sig: sig, at: kitCardsNow(), cards: cards}
	kitCardsKept.Unlock()
	writeKitCardsDisk(dir, sig, cards)
	return cards
}

// recheckKitCards reads the cards again in the background, once at a time per folder, while the kept ones answer.
// Called with kitCardsKept held.
func recheckKitCards(dir, sig string) {
	if kitCardsKept.reading[dir] {
		return
	}
	kitCardsKept.reading[dir] = true
	go func() {
		readKitCards(dir, sig)
		kitCardsKept.Lock()
		delete(kitCardsKept.reading, dir)
		kitCardsKept.Unlock()
	}()
}

// CachedKitCards is KitCards of the folder, kept while the folder looks the same (see the top of the file).
func CachedKitCards(dir string) []KitCard {
	sig := kitsSignature(dir)
	kitCardsKept.Lock()
	if e := kitCardsKept.by[dir]; e != nil && e.sig == sig {
		if kitCardsNow().Sub(e.at) >= kitCardsRecheck {
			recheckKitCards(dir, sig)
		}
		cards := e.cards
		kitCardsKept.Unlock()
		return cards
	}
	kitCardsKept.Unlock()
	if cards, ok := readKitCardsDisk(dir, sig); ok {
		// From the last run: they answer now, and the files under them are looked over once in the background.
		kitCardsKept.Lock()
		kitCardsKept.by[dir] = &kitCardsEntry{sig: sig, at: kitCardsNow(), cards: cards}
		recheckKitCards(dir, sig)
		kitCardsKept.Unlock()
		return cards
	}
	return readKitCards(dir, sig)
}

// WarmKitCards reads the cards of the folder once, so the first home after a start does not wait for them.
func WarmKitCards(dir string) { go CachedKitCards(dir) }

// forgetKitCards drops the kept cards (tests).
func forgetKitCards() {
	kitCardsKept.Lock()
	kitCardsKept.by = map[string]*kitCardsEntry{}
	kitCardsKept.Unlock()
}
