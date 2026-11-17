//go:build !race

// Package racetest tells a test whether it runs under the race detector. The detector slows the code 5-25 times
// (modernc.org/sqlite the most), so a check of how fast something is only logs there: CI runs every test with -race
// on shared runners, and a wall-clock limit failed now and then without anything being wrong.
package racetest

// On is true when the test binary is built with -race.
const On = false
