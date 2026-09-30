//go:build !linux && !darwin && !windows

// Package collect. environment_other - fallback hardware probes for operating
// systems benchbook can build on but does not know how to interrogate. Both
// answers are "unknown", which is honest and never blocks a run.
package collect

import (
	"context"
	"fmt"
	"runtime"
)

// -------------------------------------- Internal Helpers -------------------------------------- //

// cpuModel reports that no probe exists for this platform.
func (e *OSEnvironment) cpuModel(context.Context) (string, error) {
	return "", fmt.Errorf("no hardware probe implemented for %s", runtime.GOOS)
}

// memoryBytes reports that no probe exists for this platform.
func (e *OSEnvironment) memoryBytes(context.Context) (uint64, error) {
	return 0, fmt.Errorf("no hardware probe implemented for %s", runtime.GOOS)
}
