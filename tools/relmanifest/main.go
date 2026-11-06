// Command relmanifest makes and signs the release manifest (internal/release, docs/hub/upgrade.md).
//
//	relmanifest keygen PRIVATE_FILE PUBLIC_FILE
//	relmanifest make   -version X.Y.Z -dir DIST -out release.json [-schema 14 -min-from X.Y.Z] [-ext harnessly.machine-provider=1-1 …]
//	relmanifest sign   -key PRIVATE_FILE release.json   # writes release.json.sig
//
// `make` reads the binaries in DIST by their download names (runtime-linux-amd64, agent-…, harnessly-…, hub-…); every
// component takes the release version. The migrations of the release are declared additive with -additive (the default).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/release"
)

func main() {
	if len(os.Args) < 2 {
		fail("keygen | make | sign")
	}
	switch os.Args[1] {
	case "keygen":
		if len(os.Args) != 4 {
			fail("relmanifest keygen PRIVATE_FILE PUBLIC_FILE")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		check(err)
		check(os.WriteFile(os.Args[2], []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600))
		check(os.WriteFile(os.Args[3], []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644))
		fmt.Println("key id", release.KeyID(pub))
	case "make":
		doMake(os.Args[2:])
	case "sign":
		doSign(os.Args[2:])
	default:
		fail("keygen | make | sign")
	}
}

type extFlags map[string]release.Range

func (e extFlags) String() string { return "" }
func (e extFlags) Set(v string) error {
	name, rng, ok := strings.Cut(v, "=")
	lo, hi, ok2 := strings.Cut(rng, "-")
	a, err1 := strconv.Atoi(lo)
	b, err2 := strconv.Atoi(hi)
	if !ok || !ok2 || err1 != nil || err2 != nil {
		return fmt.Errorf("want NAME=MIN-MAX, got %q", v)
	}
	e[name] = release.Range{Min: a, Max: b}
	return nil
}

func doMake(args []string) {
	fs := flag.NewFlagSet("make", flag.ExitOnError)
	version := fs.String("version", "", "release version X.Y.Z")
	dir := fs.String("dir", "", "folder with the binaries")
	out := fs.String("out", "release.json", "")
	schema := fs.Int("schema", 0, "newest migration number")
	minFrom := fs.String("min-from", "", "oldest release that upgrades to this one directly")
	additive := fs.Bool("additive", true, "the migrations since min-from only add")
	exts := extFlags{}
	fs.Var(exts, "ext", "extension protocol range NAME=MIN-MAX (repeatable)")
	_ = fs.Parse(args)
	if *version == "" || *dir == "" || *schema <= 0 {
		fail("make needs -version, -dir and -schema")
	}
	m := release.Manifest{Schema: release.Schema, Version: *version, Components: map[string]release.Component{},
		ExtensionAPI: exts, Data: release.Data{SchemaVersion: *schema, Additive: *additive, MinFrom: *minFrom}}
	for _, name := range release.Components {
		c := release.Component{Version: *version}
		if name != release.UI {
			c.Files = map[string]release.File{}
			ents, err := filepath.Glob(filepath.Join(*dir, release.FileKey(name, "*", "*")))
			check(err)
			for _, p := range ents {
				b, err := os.ReadFile(p)
				check(err)
				s := sha256.Sum256(b)
				c.Files[filepath.Base(p)] = release.File{SHA256: hex.EncodeToString(s[:]), Size: int64(len(b))}
			}
			if len(c.Files) == 0 {
				continue
			}
			if name != release.Hub {
				c.Requires = map[string]string{}
			}
		}
		m.Components[name] = c
	}
	if rt, ok := m.Components[release.Runtime]; ok {
		if _, hasHub := m.Components[release.Hub]; hasHub {
			rt.Requires = map[string]string{release.Hub: *version}
			m.Components[release.Runtime] = rt
		}
	}
	for n, c := range m.Components {
		if len(c.Requires) == 0 {
			c.Requires = nil
			m.Components[n] = c
		}
	}
	body, err := json.MarshalIndent(m, "", "  ")
	check(err)
	_, err = release.Parse(body)
	check(err)
	check(os.WriteFile(*out, append(body, '\n'), 0o644))
}

func doSign(args []string) {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	key := fs.String("key", "", "private key file")
	_ = fs.Parse(args)
	if *key == "" || fs.NArg() != 1 {
		fail("relmanifest sign -key PRIVATE_FILE release.json")
	}
	raw, err := os.ReadFile(*key)
	check(err)
	priv, err := release.ParsePrivateKey(string(raw))
	check(err)
	body, err := os.ReadFile(fs.Arg(0))
	check(err)
	sig, err := release.Sign(priv, body)
	check(err)
	check(os.WriteFile(fs.Arg(0)+".sig", append(sig, '\n'), 0o644))
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "relmanifest:", msg)
	os.Exit(1)
}
