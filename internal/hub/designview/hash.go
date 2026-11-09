package designview

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
)

type hasher struct{ hash.Hash }

func newHasher() *hasher { return &hasher{sha256.New()} }

func (h *hasher) hex() string { return hex.EncodeToString(h.Sum(nil)) }
