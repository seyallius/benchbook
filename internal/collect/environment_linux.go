//go:build linux

// Package collect. environment_linux - Linux hardware probes, read from /proc so
// that no external binary (and no deprecated tool) is required.
package collect

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Paths and keys of the Linux procfs files used for hardware identification.
const (
	// procCPUInfo holds one stanza per logical CPU.
	procCPUInfo = "/proc/cpuinfo"
	// procMemInfo holds the memory summary.
	procMemInfo = "/proc/meminfo"
	// cpuInfoModelKey is the field name carrying the CPU marketing name.
	cpuInfoModelKey = "model name"
	// memInfoTotalKey is the field name carrying the total physical memory.
	memInfoTotalKey = "MemTotal"
	// kiloByte is the number of bytes in the kB value reported by procfs.
	kiloByte = 1024
)

// -------------------------------------- Internal Helpers -------------------------------------- //

// cpuModel reads the first "model name" line of /proc/cpuinfo.
func (e *OSEnvironment) cpuModel(context.Context) (string, error) {
	file, err := os.Open(procCPUInfo)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", procCPUInfo, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, scannerBufferBytes), scannerBufferBytes)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if !found || strings.TrimSpace(key) != cpuInfoModelKey {
			continue
		}
		return strings.TrimSpace(value), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", procCPUInfo, err)
	}
	return "", fmt.Errorf("%s contains no %q line", procCPUInfo, cpuInfoModelKey)
}

// memoryBytes reads MemTotal (reported in kB) from /proc/meminfo.
func (e *OSEnvironment) memoryBytes(context.Context) (uint64, error) {
	file, err := os.Open(procMemInfo)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", procMemInfo, err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, scannerBufferBytes), scannerBufferBytes)
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if !found || strings.TrimSpace(key) != memInfoTotalKey {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0, fmt.Errorf("%s has an empty %s value", procMemInfo, memInfoTotalKey)
		}
		kibibytes, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse %s %s: %w", procMemInfo, memInfoTotalKey, err)
		}
		return kibibytes * kiloByte, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read %s: %w", procMemInfo, err)
	}
	return 0, fmt.Errorf("%s contains no %q line", procMemInfo, memInfoTotalKey)
}

// scannerBufferBytes bounds the procfs line scanner.
const scannerBufferBytes = 1024 * 1024
