package runtimeutil

import "runtime/debug"

var freeOSMemory = debug.FreeOSMemory

// FinalizeRun runs end-of-run cleanup callbacks in order and then returns
// eligible memory back to the OS.
func FinalizeRun(cleanups ...func()) {
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup()
		}
	}

	freeOSMemory()
}
