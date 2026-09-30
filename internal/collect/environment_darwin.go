//go:build darwin

// Package collect. environment_darwin - macOS hardware probes through sysctl.
package collect

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/seyallius/benchbook/internal/runner"
)

// sysctl keys used to identify the machine.
const (
	// sysctlCPUModel is the CPU marketing name.
	sysctlCPUModel = "machdep.cpu.brand_string"
	// sysctlMemory is the installed physical memory in bytes.
	sysctlMemory = "hw.memsize"
)

// -------------------------------------- Internal Helpers -------------------------------------- //

// cpuModel runs `sysctl -n machdep.cpu.brand_string`.
func (e *OSEnvironment) cpuModel(ctx context.Context) (string, error) {
	return e.sysctl(ctx, sysctlCPUModel)
}

// memoryBytes runs `sysctl -n hw.memsize` and parses the byte count.
func (e *OSEnvironment) memoryBytes(ctx context.Context) (uint64, error) {
	raw, err := e.sysctl(ctx, sysctlMemory)
	if err != nil {
		return 0, err
	}
	memory, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sysctl %s output %q: %w", sysctlMemory, raw, err)
	}
	return memory, nil
}

// sysctl runs `sysctl -n <key>` and returns the trimmed value.
func (e *OSEnvironment) sysctl(ctx context.Context, key string) (string, error) {
	result, err := e.exec.Run(ctx, runner.Command{Name: sysctlBinary, Args: []string{"-n", key}})
	if err != nil {
		return "", fmt.Errorf("sysctl -n %s: %w", key, err)
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

// sysctlBinary is the macOS/BSD system control utility.
const sysctlBinary = "sysctl"
