package push

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"testing"
)

func dec(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291, Appendix A.
const (
	rfcPlaintext = "V2hlbiBJIGdyb3cgdXAsIEkgd2FudCB0byBiZSBhIHdhdGVybWVsb24"
	rfcASPublic  = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfcASPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcUAPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcUAPrivate = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfcAuth      = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcSalt      = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcSecret    = "kyrL1jIIOHEzg3sM2ZWRHDRB62YACZhhSlknJ672kSs"
	rfcBody      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func TestRFC8291Vector(t *testing.T) {
	as, err := ecdh.P256().NewPrivateKey(dec(t, rfcASPrivate))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(as.PublicKey().Bytes(), dec(t, rfcASPublic)) {
		t.Fatal("as public")
	}
	ua, err := ecdh.P256().NewPrivateKey(dec(t, rfcUAPrivate))
	if err != nil || !bytes.Equal(ua.PublicKey().Bytes(), dec(t, rfcUAPublic)) {
		t.Fatal("ua key")
	}
	if s, _ := as.ECDH(ua.PublicKey()); !bytes.Equal(s, dec(t, rfcSecret)) {
		t.Fatal("ecdh secret")
	}
	body, err := EncryptWith(dec(t, rfcPlaintext), dec(t, rfcUAPublic), dec(t, rfcAuth), as, dec(t, rfcSalt))
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.RawURLEncoding.EncodeToString(body); got != rfcBody {
		t.Fatalf("body\n got %s\nwant %s", got, rfcBody)
	}
	plain, err := Decrypt(body, ua, dec(t, rfcAuth))
	if err != nil || !bytes.Equal(plain, dec(t, rfcPlaintext)) {
		t.Fatalf("decrypt %q %v", plain, err)
	}
}
