package cloud

import (
	"fmt"

	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/mem"
	"google.golang.org/protobuf/encoding/protowire"
)

// SpeechKit v3 messages are encoded by hand (field numbers from stt.proto) so the huge generated module stays out of
// the build. The codec below passes plain []byte through grpc.

type rawCodec struct{}

func (rawCodec) Name() string { return "proto" }

func (rawCodec) Marshal(v any) (mem.BufferSlice, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("yandex codec: unexpected type %T", v)
	}
	return mem.BufferSlice{mem.SliceBuffer(b)}, nil
}

func (rawCodec) Unmarshal(data mem.BufferSlice, v any) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("yandex codec: unexpected type %T", v)
	}
	*p = data.Materialize()
	return nil
}

var _ encoding.CodecV2 = rawCodec{}

func appendMsg(b []byte, num protowire.Number, inner []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, inner)
}

func appendVarint(b []byte, num protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

// encodeSessionOptions builds StreamingRequest{session_options}. langs are BCP-47 codes of the whitelist.
func encodeSessionOptions(langs []string) []byte {
	var raw []byte
	raw = appendVarint(raw, 1, 1)     // LINEAR16_PCM
	raw = appendVarint(raw, 2, 16000) // sample_rate_hertz
	raw = appendVarint(raw, 3, 1)     // audio_channel_count
	format := appendMsg(nil, 1, raw)  // AudioFormatOptions.raw_audio

	norm := appendVarint(nil, 1, 1) // TEXT_NORMALIZATION_ENABLED

	var lr []byte
	lr = appendVarint(lr, 1, 1) // WHITELIST
	for _, l := range langs {
		lr = protowire.AppendTag(lr, 2, protowire.BytesType)
		lr = protowire.AppendString(lr, l)
	}

	var model []byte
	model = protowire.AppendTag(model, 1, protowire.BytesType)
	model = protowire.AppendString(model, "general")
	model = appendMsg(model, 2, format)
	model = appendMsg(model, 3, norm)
	model = appendMsg(model, 4, lr)
	model = appendVarint(model, 5, 1) // REAL_TIME

	eou := appendMsg(nil, 1, appendVarint(nil, 1, 1)) // default_classifier{type: DEFAULT}

	var opts []byte
	opts = appendMsg(opts, 1, model)
	opts = appendMsg(opts, 2, eou)
	return appendMsg(nil, 1, opts)
}

// encodeChunk builds StreamingRequest{chunk: AudioChunk{data}}.
func encodeChunk(data []byte) []byte {
	inner := protowire.AppendTag(nil, 1, protowire.BytesType)
	inner = protowire.AppendBytes(inner, data)
	return appendMsg(nil, 2, inner)
}

// ysResponse is the part of StreamingResponse the recognizer uses.
type ysResponse struct {
	Partial    *string // partial: first alternative's text
	Final      *string // final: first alternative's text
	FinalIndex int64   // audio_cursors.final_index
	RefineIdx  int64   // final_refinement.final_index
	Refined    *string // final_refinement.normalized_text: first alternative's text
}

func walk(b []byte, fn func(num protowire.Number, typ protowire.Type, val []byte, v uint64)) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]
		var val []byte
		var v uint64
		switch typ {
		case protowire.BytesType:
			val, n = protowire.ConsumeBytes(b)
		case protowire.VarintType:
			v, n = protowire.ConsumeVarint(b)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return protowire.ParseError(n)
		}
		fn(num, typ, val, v)
		b = b[n:]
	}
	return nil
}

// altUpdateText returns the text of the first alternative of an AlternativeUpdate.
func altUpdateText(b []byte) (string, error) {
	var text string
	seen := false
	var inner error
	err := walk(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if num != 1 || typ != protowire.BytesType || seen {
			return
		}
		seen = true
		inner = walk(val, func(n protowire.Number, t protowire.Type, v []byte, _ uint64) {
			if n == 2 && t == protowire.BytesType {
				text = string(v)
			}
		})
	})
	if err == nil {
		err = inner
	}
	return text, err
}

func decodeResponse(b []byte) (ysResponse, error) {
	var r ysResponse
	var ferr error
	set := func(dst **string, v []byte) {
		t, err := altUpdateText(v)
		if err != nil {
			ferr = err
		}
		*dst = &t
	}
	err := walk(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if typ != protowire.BytesType {
			return
		}
		switch num {
		case 2: // audio_cursors
			if err := walk(val, func(n protowire.Number, t protowire.Type, _ []byte, v uint64) {
				if n == 5 && t == protowire.VarintType {
					r.FinalIndex = int64(v)
				}
			}); err != nil {
				ferr = err
			}
		case 4:
			set(&r.Partial, val)
		case 5:
			set(&r.Final, val)
		case 7: // final_refinement
			if err := walk(val, func(n protowire.Number, t protowire.Type, v []byte, x uint64) {
				switch {
				case n == 1 && t == protowire.VarintType:
					r.RefineIdx = int64(x)
				case n == 2 && t == protowire.BytesType:
					set(&r.Refined, v)
				}
			}); err != nil {
				ferr = err
			}
		}
	})
	if err == nil {
		err = ferr
	}
	return r, err
}
