package providers

import (
	"errors"
	"os"
)

// A profile's switch is on until the person turns it off: the models of every vendor that can run in Claude
// Code are offered there by default. The off is what is kept, in offFile; onFile is still written when the switch goes
// on, so a runtime from before reads the same. A switch with no offFile is on only while onFile exists (as before).
// A switch turned off by a runtime from before left neither file: it reads as on, the only way to tell «never touched».

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// switchOn reads a switch from its files.
func switchOn(onFile, offFile string) bool {
	if offFile == "" {
		return fileExists(onFile)
	}
	return !fileExists(offFile)
}

// writeSwitch keeps a switch's state in its files: on removes offFile and writes onFile, off the other way round.
func writeSwitch(onFile, offFile string, on bool) error {
	drop, put := offFile, onFile
	if !on {
		drop, put = onFile, offFile
	}
	if drop != "" {
		if err := os.Remove(drop); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if put == "" {
		return nil
	}
	return writePrivate(put, nil)
}
