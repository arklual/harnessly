//go:build linux

package agent

import "testing"

func TestCPUModelAndChassis(t *testing.T) {
	if got := cpuModel("processor : 0\nmodel name\t: Example  CPU @ 3.0GHz\n"); got != "Example CPU @ 3.0GHz" {
		t.Fatalf("%q", got)
	}
	if got := cpuModel("Hardware : BCM2711\n"); got != "BCM2711" {
		t.Fatalf("%q", got)
	}
	if !laptopChassis(10) || laptopChassis(3) || laptopChassis(1) {
		t.Fatal("chassis types")
	}
}
