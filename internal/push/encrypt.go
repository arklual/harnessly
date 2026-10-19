package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// recordSize is the aes128gcm record size the push is sent with (one record).
const recordSize = 4096

// keys derives the content encryption key and nonce of RFC 8291 §3.4 and RFC 8188 §2.2.
func keys(ecdhSecret, authSecret, uaPublic, asPublic, salt []byte) (cek, nonce []byte, err error) {
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPublic...), asPublic...)
	ikm, err := hkdf.Key(sha256.New, ecdhSecret, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, err
	}
	if cek, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	return cek, nonce, err
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptWith is RFC 8291 message encryption with a given sender key and salt (the test vector's); Encrypt draws them.
// uaPublic is the subscription's p256dh key (65 bytes, uncompressed), authSecret its auth secret (16 bytes). The result
// is the aes128gcm body: salt, record size, key id (the sender's public key) and one record.
func EncryptWith(plaintext, uaPublic, authSecret []byte, asPrivate *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(salt) != 16 {
		return nil, errors.New("push: salt must be 16 bytes")
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, errors.New("push: bad p256dh key")
	}
	secret, err := asPrivate.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := asPrivate.PublicKey().Bytes()
	cek, nonce, err := keys(secret, authSecret, uaPublic, asPublic, salt)
	if err != nil {
		return nil, err
	}
	aead, err := gcm(cek)
	if err != nil {
		return nil, err
	}
	if len(plaintext)+1+aead.Overhead() > recordSize {
		return nil, errors.New("push: message too long for one record")
	}
	header := make([]byte, 0, 21+len(asPublic))
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, recordSize)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	// The last (only) record ends with the 0x02 delimiter and no padding.
	record := append(append([]byte{}, plaintext...), 2)
	return aead.Seal(header, nonce, record, nil), nil
}

// Encrypt encrypts a message for a subscription with a fresh sender key and salt.
func Encrypt(plaintext, uaPublic, authSecret []byte) ([]byte, error) {
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return EncryptWith(plaintext, uaPublic, authSecret, as, salt)
}

// Decrypt is the user agent's side, for tests and a fake push service: it opens an aes128gcm body with the
// subscription's private key and auth secret.
func Decrypt(body []byte, uaPrivate *ecdh.PrivateKey, authSecret []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("push: short body")
	}
	salt, idLen := body[:16], int(body[20])
	if len(body) < 21+idLen {
		return nil, errors.New("push: short body")
	}
	asPublic := body[21 : 21+idLen]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		return nil, err
	}
	secret, err := uaPrivate.ECDH(as)
	if err != nil {
		return nil, err
	}
	cek, nonce, err := keys(secret, authSecret, uaPrivate.PublicKey().Bytes(), asPublic, salt)
	if err != nil {
		return nil, err
	}
	aead, err := gcm(cek)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, body[21+idLen:], nil)
	if err != nil {
		return nil, err
	}
	// Strip the padding and the delimiter.
	for i := len(plain) - 1; i >= 0; i-- {
		if plain[i] == 2 || plain[i] == 1 {
			return plain[:i], nil
		}
		if plain[i] != 0 {
			break
		}
	}
	return nil, errors.New("push: no record delimiter")
}
