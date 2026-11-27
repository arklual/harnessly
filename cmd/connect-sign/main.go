// connect-sign makes and uses the key Harnessly Connect's updates are signed with: Connect takes a release only when
// its checksums.txt carries this key's signature.
//
//	go run ./cmd/connect-sign keygen            prints a new private key (for the CI secret) and its public key
//	CONNECT_SIGNING_KEY=… go run ./cmd/connect-sign sign checksums.txt   writes checksums.txt.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "keygen" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		fmt.Println("private (secret CONNECT_SIGNING_KEY):", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("public  (variable CONNECT_UPDATE_KEY):", base64.StdEncoding.EncodeToString(pub))
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "sign" {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("CONNECT_SIGNING_KEY")))
		if err != nil || len(seed) != ed25519.SeedSize {
			fail(fmt.Errorf("CONNECT_SIGNING_KEY is not a base64 Ed25519 seed"))
		}
		raw, err := os.ReadFile(os.Args[2])
		if err != nil {
			fail(err)
		}
		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), raw)
		if err := os.WriteFile(os.Args[2]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
			fail(err)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "usage: connect-sign keygen | connect-sign sign FILE")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "connect-sign:", err)
	os.Exit(1)
}
