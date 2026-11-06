package env

// SetMaxDeliveryMachines changes the bound past which the screen's delivery is not computed; it returns the undo.
func SetMaxDeliveryMachines(n int) func() {
	old := maxDeliveryMachines
	maxDeliveryMachines = n
	return func() { maxDeliveryMachines = old }
}
