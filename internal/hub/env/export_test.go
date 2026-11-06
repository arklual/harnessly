package env

import "time"

// SetMaxDeliveryMachines changes the bound past which the screen's delivery is not computed; it returns the undo.
func SetMaxDeliveryMachines(n int) func() {
	old := maxDeliveryMachines
	maxDeliveryMachines = n
	return func() { maxDeliveryMachines = old }
}

// SetApplyDelay changes how long a machine that just connected waits before its pending run; it returns the undo.
func SetApplyDelay(d time.Duration) func() {
	old := applyDelay
	applyDelay = d
	return func() { applyDelay = old }
}
