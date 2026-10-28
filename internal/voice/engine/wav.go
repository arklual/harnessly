package engine

import (
	"encoding/binary"
	"math"
)

const sampleRate = 16000

// wav wraps mono 16 kHz signed 16-bit little-endian samples in a WAV container.
func wav(pcm []byte) []byte {
	b := make([]byte, 44, 44+len(pcm))
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], 1) // mono
	binary.LittleEndian.PutUint32(b[24:], sampleRate)
	binary.LittleEndian.PutUint32(b[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)))
	return append(b, pcm...)
}

// rms is the loudness of PCM16 samples, 0..1.
func rms(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}
