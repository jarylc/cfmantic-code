package runtimeutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFinalizeRun_RunsCleanupsBeforeFreeOSMemory(t *testing.T) {
	originalFreeOSMemory := freeOSMemory

	t.Cleanup(func() { freeOSMemory = originalFreeOSMemory })

	var order []string

	freeOSMemory = func() {
		order = append(order, "free")
	}

	FinalizeRun(
		func() { order = append(order, "lock") },
		func() { order = append(order, "run") },
		func() { order = append(order, "guard") },
	)

	assert.Equal(t, []string{"lock", "run", "guard", "free"}, order)
}

func TestFinalizeRun_IgnoresNilCleanup(t *testing.T) {
	originalFreeOSMemory := freeOSMemory

	t.Cleanup(func() { freeOSMemory = originalFreeOSMemory })

	freeCalls := 0
	freeOSMemory = func() {
		freeCalls++
	}

	var cleanup func()

	assert.NotPanics(t, func() {
		FinalizeRun(cleanup)
	})
	assert.Equal(t, 1, freeCalls)
}
