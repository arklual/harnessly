package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
)

var iconCache = map[bool][]byte{}

// icon is the tray's mark: a ring with a dot, green when linked; ICO on Windows, PNG elsewhere.
func icon(online bool) []byte {
	if b, ok := iconCache[online]; ok {
		return b
	}
	const n = 32
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	ring := color.NRGBA{120, 120, 116, 255}
	dot := color.NRGBA{150, 150, 146, 255}
	if online {
		ring, dot = color.NRGBA{47, 125, 79, 255}, color.NRGBA{63, 170, 106, 255}
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			d := math.Hypot(float64(x)-15.5, float64(y)-15.5)
			switch {
			case d >= 11 && d <= 14.5:
				img.Set(x, y, ring)
			case d <= 6:
				img.Set(x, y, dot)
			}
		}
	}
	var pb bytes.Buffer
	_ = png.Encode(&pb, img)
	out := pb.Bytes()
	if runtime.GOOS == "windows" {
		// An ICO holding one PNG image.
		var ib bytes.Buffer
		_ = binary.Write(&ib, binary.LittleEndian, []uint16{0, 1, 1})
		ib.Write([]byte{n, n, 0, 0})
		_ = binary.Write(&ib, binary.LittleEndian, []uint16{1, 32})
		_ = binary.Write(&ib, binary.LittleEndian, []uint32{uint32(len(out)), 22})
		ib.Write(out)
		out = ib.Bytes()
	}
	iconCache[online] = out
	return out
}
