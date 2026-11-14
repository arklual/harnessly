//go:build race

package searchindex

// raceOn: the race detector slows SQLite tenfold and more, so the timing check only logs.
const raceOn = true
