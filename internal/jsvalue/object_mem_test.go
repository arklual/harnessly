package jsvalue

import (
	"fmt"
	"runtime"
	"testing"
)

// A wide object past indexAt keeps working through its index, in the order keys were set.
func TestObjectWideKeepsOrderAndLookup(t *testing.T) {
	o := NewObject()
	for i := 0; i < 40; i++ {
		o.Set(fmt.Sprintf("k%02d", i), float64(i))
	}
	o.Set("k05", "again")
	o.Delete("k10")
	o.Delete("missing")
	if o.Len() != 39 {
		t.Fatalf("len %d", o.Len())
	}
	if v, _ := o.Get("k05"); v != "again" {
		t.Fatalf("k05 = %v", v)
	}
	if o.Has("k10") {
		t.Fatal("k10 still there")
	}
	if v, _ := o.Get("k39"); v != 39.0 {
		t.Fatalf("k39 = %v", v)
	}
	keys := o.Keys()
	if keys[0] != "k00" || keys[10] != "k11" || keys[38] != "k39" {
		t.Fatalf("order %v", keys)
	}
	c := Clone(o).(*Object)
	if !SameValue(o, c) || !Equal(o, c) {
		t.Fatal("clone differs")
	}
	c.Set("extra", true)
	if o.Has("extra") || !c.Has("extra") {
		t.Fatal("clone shares state")
	}
}

// BenchmarkSmallObjects is what a transcript entry costs: many objects of a few keys. B/obj is the heap kept per object.
func BenchmarkSmallObjects(b *testing.B) {
	const n = 20000
	var keep []*Object
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i < b.N; i++ {
		keep = keep[:0]
		for j := 0; j < n; j++ {
			keep = append(keep, NewObject("type", "assistant", "uuid", "u", "timestamp", "t", "role", "r", "n", float64(j)))
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/n, "B/obj")
	runtime.KeepAlive(keep)
}
