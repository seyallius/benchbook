//go:build windows

// Package collect. environment_windows - Windows hardware probes through
// PowerShell Get-CimInstance. wmic is deprecated and absent on current Windows
// builds, so it is never used; when PowerShell is unavailable the probe returns
// "unknown" and the run continues.
package collect

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/seyallius/benchbook/internal/runner"
)

// PowerShell invocation and queries.
const (
	// powershellBinary is the executable name resolved from PATH.
	powershellBinary = "powershell"
	// powershellNoProfile skips user profiles so output stays machine-readable.
	powershellNoProfile = "-NoProfile"
	// powershellNonInteractive forbids prompts that would hang the run.
	powershellNonInteractive = "-NonInteractive"
	// powershellCommand introduces the script to execute.
	powershellCommand = "-Command"
	// cpuModelQuery selects the processor name (CIM, not the deprecated wmic).
	cpuModelQuery = "(Get-CimInstance Win32_Processor | Select-Object -First 1 -ExpandProperty Name)"
	// memoryQuery selects the installed physical memory in bytes.
	memoryQuery = "(Get-CimInstance Win32_ComputerSystem | Select-Object -First 1 -ExpandProperty TotalPhysicalMemory)"
)

// -------------------------------------- Internal Helpers -------------------------------------- //

// cpuModel runs a CIM query for the processor name.
func (e *OSEnvironment) cpuModel(ctx context.Context) (string, error) {
	return e.powershell(ctx, cpuModelQuery)
}

// memoryBytes runs a CIM query for the installed memory.
func (e *OSEnvironment) memoryBytes(ctx context.Context) (uint64, error) {
	raw, err := e.powershell(ctx, memoryQuery)
	if err != nil {
		return 0, err
	}
	memory, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse PowerShell memory output %q: %w", raw, err)
	}
	return memory, nil
}

// powershell runs one query and returns the trimmed single-line result.
func (e *OSEnvironment) powershell(ctx context.Context, query string) (string, error) {
	command := runner.Command{Name: powershellBinary, Args: []string{
		powershellNoProfile, powershellNonInteractive, powershellCommand, query,
	}}
	result, err := e.exec.Run(ctx, command)
	if err != nil {
		return "", fmt.Errorf("powershell %s: %w", query, err)
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}
