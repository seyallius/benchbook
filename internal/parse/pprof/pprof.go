// Package pprof. pprof - parser for `go tool pprof -top` text output. It converts
// every value to base units (nanoseconds, bytes or object counts) so that the
// stored data is unit-free and only the renderer decides how to display it.
package pprof

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Header prefixes that pprof prints above the table. They are kept verbatim in
// Profile.Meta so a report can show exactly what the profiler said.
const (
	// headerFile names the profiled binary.
	headerFile = "File:"
	// headerBuildID identifies the profiled binary.
	headerBuildID = "Build ID:"
	// headerType names the profile kind ("cpu", "heap", ...).
	headerType = "Type:"
	// headerTime is the capture wall-clock time.
	headerTime = "Time:"
	// headerDuration reports the sampling duration and coverage.
	headerDuration = "Duration:"
	// headerShowing summarises how much of the total the rows cover.
	headerShowing = "Showing nodes accounting for"
	// headerDropped reports how many small nodes were dropped.
	headerDropped = "Dropped"
	// headerShowingTop reports how many of the nodes are printed.
	headerShowingTop = "Showing top"
)

// HeaderPrefixes lists every recognised metadata prefix, in no particular order.
var HeaderPrefixes = []string{
	headerFile, headerBuildID, headerType, headerTime, headerDuration,
	headerShowing, headerShowingTop, headerDropped,
}

// Number of columns in a table row before the symbol name, and the minimum number
// of fields a row must have (5 numeric columns plus a name).
const (
	// rowNameOffset is the field index at which the symbol name starts.
	rowNameOffset = 5
	// minRowFields is the smallest field count of a data row.
	minRowFields = 6
)

// Units and their multipliers, expressed in base units of their family. pprof uses
// binary multiples for byte units (kB = 1024 B) and SI multiples for counts.
const (
	// nano is the base unit of time (nanoseconds).
	nano = 1.0
	// micro is one microsecond in nanoseconds.
	micro = 1e3
	// milli is one millisecond in nanoseconds.
	milli = 1e6
	// second is one second in nanoseconds.
	second = 1e9
	// minute is one minute in nanoseconds.
	minute = 60 * second
	// hour is one hour in nanoseconds.
	hour = 60 * minute

	// byteUnit is the base unit of the space family.
	byteUnit = 1.0
	// kibi is 1 kB in bytes, following pprof's binary interpretation.
	kibi = 1024
	// mebi is 1 MB in bytes.
	mebi = kibi * 1024
	// gibi is 1 GB in bytes.
	gibi = mebi * 1024
	// tebi is 1 TB in bytes.
	tebi = gibi * 1024

	// countUnit is the base unit of the count family.
	countUnit = 1.0
	// kilo is 1 k in counts.
	kilo = 1e3
	// mega is 1 M in counts.
	mega = 1e6
	// giga is 1 G in counts.
	giga = 1e9
)

// timeUnits and byteUnits map the suffixes pprof may print onto their multipliers.
var timeUnits = map[string]float64{
	"ns": nano,
	"us": micro,
	"µs": micro,
	"μs": micro,
	"ms": milli,
	"s":  second,
	"m":  minute,
	"h":  hour,
}

// byteUnits maps byte suffixes onto their multipliers.
var byteUnits = map[string]float64{
	"B":  byteUnit,
	"kB": kibi,
	"KB": kibi,
	"MB": mebi,
	"GB": gibi,
	"TB": tebi,
}

// countUnits maps count suffixes onto their multipliers.
var countUnits = map[string]float64{
	"k": kilo,
	"K": kilo,
	"M": mega,
	"G": giga,
}

// Options configures a Parser.
type Options struct {
	// Source is the file name reported in error messages.
	Source string
	// Kind is the profile kind, which decides the base unit of unit-less values.
	Kind model.ProfileKind
	// Target is ProfileTargetFull or the benchmark name the profile belongs to.
	Target string
}

// Parser is the pprof -top implementation of model.ProfileParser.
type Parser struct {
	options Options
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// unitValue splits a printed value into its numeric part and its unit suffix.
var unitValue = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?)([A-Za-zµμ%]*)$`)

// ---------------------------------------- Constructor(s) -------------------------------------- //

// New returns a parser for `go tool pprof -top` text.
func New(options Options) *Parser {
	if strings.TrimSpace(options.Source) == "" {
		options.Source = "pprof output"
	}
	if options.Target == "" {
		options.Target = model.ProfileTargetFull
	}
	return &Parser{options: options}
}

// ----------------------------------------- Public API ----------------------------------------- //

// Parse reads one `pprof -top` table. Unknown lines are ignored, so a file with
// extra banners, warnings or a missing "Showing nodes" line still parses.
func (p *Parser) Parse(reader io.Reader) (*model.Profile, error) {
	profile := &model.Profile{
		Kind:   p.options.Kind,
		Target: p.options.Target,
		Meta:   []string{},
		Rows:   []model.ProfileRow{},
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, initialBufferBytes), maxLineBytes)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimRight(scanner.Text(), " \t\r")
		if isHeaderLine(line) {
			profile.Meta = append(profile.Meta, strings.TrimSpace(line))
			continue
		}
		if isColumnHeader(line) {
			continue
		}
		row, ok := p.parseRow(line)
		if !ok {
			continue
		}
		profile.Rows = append(profile.Rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", p.options.Source, err)
	}
	if len(profile.Rows) == 0 {
		return nil, model.NewParserError(p.options.Source,
			"a `go tool pprof -top` table with rows like `1.20s 22.10% 22.10% 2.40s 40.00%  pkg.Func`")
	}
	return profile, nil
}

// Buffer limits for the line scanner.
const (
	// initialBufferBytes is the initial scanner buffer size.
	initialBufferBytes = 64 * 1024
	// maxLineBytes bounds a single input line.
	maxLineBytes = 4 * 1024 * 1024
)

// isHeaderLine reports whether a line is pprof metadata rather than table data.
func isHeaderLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range HeaderPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// isColumnHeader reports whether a line is the "flat flat% sum% cum cum%" header.
func isColumnHeader(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < rowNameOffset {
		return false
	}
	return strings.HasPrefix(fields[0], "flat") && strings.HasPrefix(fields[2], "sum")
}

// parseRow converts one table row into a ProfileRow in base units.
func (p *Parser) parseRow(line string) (model.ProfileRow, bool) {
	fields := strings.Fields(line)
	if len(fields) < minRowFields {
		return model.ProfileRow{}, false
	}
	flat, err := p.value(fields[0])
	if err != nil {
		return model.ProfileRow{}, false
	}
	flatPct, err := percentage(fields[1])
	if err != nil {
		return model.ProfileRow{}, false
	}
	sumPct, err := percentage(fields[2])
	if err != nil {
		return model.ProfileRow{}, false
	}
	cum, err := p.value(fields[3])
	if err != nil {
		return model.ProfileRow{}, false
	}
	cumPct, err := percentage(fields[4])
	if err != nil {
		return model.ProfileRow{}, false
	}
	return model.ProfileRow{
		Flat:    flat,
		FlatPct: flatPct,
		SumPct:  sumPct,
		Cum:     cum,
		CumPct:  cumPct,
		Name:    strings.Join(fields[rowNameOffset:], " "),
	}, true
}

// value converts a printed value into base units. The unit suffix decides the
// family; a unit-less value uses the family implied by the profile kind.
func (p *Parser) value(raw string) (float64, error) {
	match := unitValue.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return 0, fmt.Errorf("value %q is not a number with an optional unit", raw)
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse value %q: %w", raw, err)
	}
	suffix := match[2]
	if suffix == "" {
		return number * p.baseUnit(), nil
	}
	for _, table := range []map[string]float64{timeUnits, byteUnits, countUnits} {
		if multiplier, ok := table[suffix]; ok {
			return number * multiplier, nil
		}
	}
	return 0, fmt.Errorf("unit %q in value %q is not a known time, byte or count unit", suffix, raw)
}

// baseUnit returns the multiplier of a unit-less value for this profile kind.
func (p *Parser) baseUnit() float64 {
	switch p.options.Kind {
	case model.ProfileKindMemSpace:
		return byteUnit
	case model.ProfileKindMemObjects:
		return countUnit
	default:
		return nano
	}
}

// percentage parses a "12.34%" cell.
func percentage(raw string) (float64, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "%")
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		if math.IsNaN(value) {
			return 0, fmt.Errorf("percentage %q is not a number", raw)
		}
		return 0, fmt.Errorf("parse percentage %q: %w", raw, err)
	}
	return value, nil
}
