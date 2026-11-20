package main

import "testing"

// A filesystem that reports sizes no disk has gives «unknown» (0, 0), never a number the page cannot read (GitHub #119).
func TestDiskSizes(t *testing.T) {
	cases := []struct {
		blocks, avail, bsize uint64
		total, free          int64
	}{
		{1000, 250, 4096, 4096000, 1024000},
		{1 << 62, 1, 4096, 0, 0},               // the product overflows
		{1 << 45, 1, 1 << 12, 0, 0},            // past 2^53
		{100, 200, 4096, 0, 0},                 // more free than there is
		{0, 0, 4096, 0, 0}, {100, 10, 0, 0, 0}, // nothing measured
		{maxSafeInteger, 0, 1, maxSafeInteger, 0}, // the edge still fits
	}
	for _, c := range cases {
		if total, free := diskSizes(c.blocks, c.avail, c.bsize); total != c.total || free != c.free {
			t.Errorf("%d×%d free %d: %d %d", c.blocks, c.bsize, c.avail, total, free)
		}
	}
}
