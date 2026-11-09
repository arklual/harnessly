package hlcli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// Where `pack --sign` finds the private key: the environment, put there by the safe (`harnessly secret run --env`), so
// that no file or chat holds it.
const signingKeyEnv = "HLAPP_SIGNING_KEY"

// appPack: harnessly app pack [DIR] [-o FILE] [--sign] [--publisher NAME] — the package as a .hlapp, signed with --sign.
func (c *cli) appPack(args []string) error {
	dir, out, publisher, sign := "", "", "", false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--sign":
			sign = true
		case a == "-o" || a == "--out" || a == "--publisher":
			if i+1 >= len(args) {
				return usageErr("%s needs a value", a)
			}
			i++
			if a == "--publisher" {
				publisher = args[i]
			} else {
				out = args[i]
			}
		case strings.HasPrefix(a, "-"):
			return usageErr("unknown flag %s", a)
		case dir == "":
			dir = a
		default:
			return usageErr("pack takes one folder")
		}
	}
	dir = c.appDir(dir)
	pkg, ps := apps.LoadPackage(dir)
	if pkg == nil || len(ps.Errors()) > 0 {
		for _, p := range ps {
			c.out("%s\n", p)
		}
		return fail("not packed: the package has errors (harnessly app validate %s)", dir)
	}
	raw, sum, err := apps.Pack(dir)
	if err != nil {
		return fail("not packed: %v", err)
	}
	if out == "" {
		out = pkg.Manifest.ID + "-" + pkg.Manifest.Version + ".hlapp"
	}
	out = c.absPath(out)
	var sig *apps.Signature
	if sign {
		key := strings.TrimSpace(c.env.Getenv(signingKeyEnv))
		if key == "" {
			return fail("no signing key: the key lives in the safe, not in a file. Run\n  harnessly secret run --env %s=hlapp-signing-key -- harnessly app pack --sign\n"+
				"(a key is made by `harnessly app key`)", signingKeyEnv)
		}
		if publisher == "" {
			publisher, _, _ = strings.Cut(pkg.Manifest.ID, ".")
		}
		s, err := apps.Sign(raw, publisher, key)
		if err != nil {
			return fail("not signed: %v", err)
		}
		sig = &s
	}
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		return fail("not written: %v", err)
	}
	if sig != nil {
		if err := os.WriteFile(out+apps.SigSuffix, sig.Marshal(), 0o644); err != nil {
			return fail("signature not written: %v", err)
		}
	}
	if c.json {
		c.printMap(map[string]any{"file": out, "sha256": sum, "signed": sig != nil, "signature": sigPath(out, sig)})
		return nil
	}
	if sig != nil {
		c.out("%s (%d bytes), signed by %s, key %s\nthe signature is in %s: give both files\n", filepath.Base(out), len(raw), sig.Publisher, apps.KeyID(sig.Key), filepath.Base(out)+apps.SigSuffix)
	} else {
		c.out("%s (%d bytes), not signed (--sign signs it)\n", filepath.Base(out), len(raw))
	}
	return nil
}

func (c *cli) printMap(v map[string]any) {
	raw, _ := json.Marshal(v)
	c.printRaw(raw)
}

func sigPath(out string, sig *apps.Signature) string {
	if sig == nil {
		return ""
	}
	return out + apps.SigSuffix
}

// appKey: harnessly app key --out FILE — a publisher's key pair. The public key is printed (the deployment's admin adds
// it to the trusted publishers); the private one goes into FILE (0600), to put in the safe and delete.
func (c *cli) appKey(args []string) error {
	out := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--out" || a == "-o":
			if i+1 >= len(args) {
				return usageErr("%s needs a value", a)
			}
			i++
			out = args[i]
		default:
			return usageErr("key takes --out FILE")
		}
	}
	if out == "" {
		return usageErr("key needs --out FILE for the private key (it is never printed)")
	}
	out = c.absPath(out)
	priv, pub, err := apps.GenerateKey()
	if err != nil {
		return fail("%v", err)
	}
	if err := os.WriteFile(out, []byte(priv+"\n"), 0o600); err != nil {
		return fail("not written: %v", err)
	}
	if c.json {
		c.printMap(map[string]any{"publicKey": pub, "keyId": apps.KeyID(pub), "privateKeyFile": out})
		return nil
	}
	c.out("public key (give it to the admin of the hub): %s\nkey id: %s\nthe private key is in %s. Put it in the safe and delete the file:\n"+
		"  harnessly secret ask hlapp-signing-key --why \"signing of app packages\"   (paste the file's content)\n  rm %s\n",
		pub, apps.KeyID(pub), out, out)
	return nil
}
