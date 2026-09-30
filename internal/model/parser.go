// Package model. parser - the input-adapter seam. Both interfaces live here,
// next to the types they produce, so a future adapter only needs this package.
package model

import "io"

// ---------------------------------------------- Types ----------------------------------------- //

// Parser turns raw benchmark text into canonical benchmarks. The Go `go test
// -bench` parser is the only implementation benchbook ships; the interface exists
// so another harness could be plugged in without touching the CLI, the store or
// the renderers.
type Parser interface {
	// Parse reads raw benchmark output and returns one Benchmark per benchmark id,
	// each carrying all observed samples.
	Parse(reader io.Reader) ([]Benchmark, error)
}

// ProfileParser turns raw profiler text into canonical profiles.
type ProfileParser interface {
	// Parse reads raw profiler output and returns the profiles it contains.
	ParseProfile(reader io.Reader) (*Profile, error)
}
