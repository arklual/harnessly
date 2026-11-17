package racetest

import "os"

// StrictTiming says whether a wall-clock limit of a test should fail it: on a developer's machine yes; under the race
// detector, or on CI where the test shares a CPU quota with the other packages, the time only goes to the log.
func StrictTiming() bool { return !On && os.Getenv("CI") == "" }
