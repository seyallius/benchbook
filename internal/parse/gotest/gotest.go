// Package gotest. gotest - parser for `go test -run '^$' -bench=.` output. It is
// the only benchmark input adapter benchbook ships (see model.Parser for the seam).
package gotest

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/seyallius/benchbook/internal/model"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Unit names of the standard columns. They are the units `go test` always uses,
// which is why they can be matched literally instead of by column position.
const (
	// UnitNsOp is the wall-time column.
	UnitNsOp = "ns/op"
	// UnitBytesOp is the allocated-bytes column, present only with -benchmem.
	UnitBytesOp = "B/op"
	// UnitAllocsOp is the allocations column, present only with -benchmem.
	UnitAllocsOp = "allocs/op"
)

// Prefixes and markers of the lines `go test` interleaves with benchmark results.
const (
	// pkgPrefix marks the package the following benchmark lines belong to.
	pkgPrefix = "pkg:"
	// benchPrefix is the mandatory prefix of a Go benchmark name.
	benchPrefix = model.BenchPrefix
)

// GroupRule is the legacy fallback mapping for flat (non sub-benchmark) names. It
// mirrors the config type structurally; parsing deliberately does not import the
// config package, so the CLI converts between the two. See DECISIONS.md.
type GroupRule struct {
	// Contains must appear in the name, on word boundaries, for the rule to apply.
	Contains string
	// Exclude disqualifies an otherwise matching name.
	Exclude string
	// Workload is the workload name assigned to matching benchmarks.
	Workload string
}

// Options configures a Parser.
type Options struct {
	// Source is the file name reported in error messages, e.g. "benchbook/raw/run.txt".
	Source string
	// GroupRules apply to flat benchmark names only; see section 7 of the spec.
	GroupRules []GroupRule
	// DefaultPackage is used when the input has no "pkg:" line.
	DefaultPackage string
}

// Parser is the `go test -bench` implementation of model.Parser.
type Parser struct {
	options Options
}

// -------------------------------------- Internal Helpers -------------------------------------- //

// gomaxprocsSuffix matches the optional "-<digits>" the testing package appends to
// a benchmark name (the GOMAXPROCS value). Without stripping it, "-8" would become
// part of the benchmark's identity and every machine would look like a different
// benchmark.
var gomaxprocsSuffix = regexp.MustCompile(`-(\d+)$`)

// Numeric scan of a benchmark line: the name, an optional GOMAXPROCS suffix, an
// optional iteration count and at least one "<value> <unit>" metric pair.
//
//	BenchmarkFindUser/result-12   80904   15276 ns/op   1040 B/op   34 allocs/op
const minBenchFields = 2

// ---------------------------------------- Constructor(s) -------------------------------------- //

// New returns a parser for raw `go test -bench` text.
func New(options Options) *Parser {
	if strings.TrimSpace(options.Source) == "" {
		options.Source = "benchmark output"
	}
	return &Parser{options: options}
}

// ----------------------------------------- Public API ----------------------------------------- //

// Parse implements model.Parser. It never fails on unrecognised lines: container
// set-up logs, `PASS`, `ok   pkg  1.2s`, coverage reports and CI banners are all
// ignored silently. It fails only when the input contained no benchmark at all,
// which always means the command or the fixture is wrong and never means "no
// change".
func (p *Parser) Parse(reader io.Reader) ([]model.Benchmark, error) {
	byID := map[string]*model.Benchmark{}
	order := []string{}
	packageName := p.options.DefaultPackage
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, initialBufferBytes), maxLineBytes)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t\r")
		if name, ok := parsePackageLine(line); ok {
			packageName = name
			continue
		}
		sample, ok := parseBenchmarkLine(line)
		if !ok {
			continue
		}
		benchmark := p.benchmarkFor(byID, &order, packageName, sample.name)
		benchmark.Samples = append(benchmark.Samples, sample.sample)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", p.options.Source, err)
	}
	if len(order) == 0 {
		return nil, model.NewParserError(p.options.Source,
			"lines like `BenchmarkFindUser/result-12  80904  15276 ns/op  1040 B/op  34 allocs/op`; "+
				"make sure `go test -bench=.` actually ran benchmarks")
	}
	benchmarks := make([]model.Benchmark, 0, len(order))
	for _, id := range order {
		benchmarks = append(benchmarks, *byID[id])
	}
	model.SortBenchmarks(benchmarks)
	return benchmarks, nil
}

// Size limits of the line scanner.
const (
	// initialBufferBytes is the initial scanner buffer size.
	initialBufferBytes = 64 * 1024
	// maxLineBytes bounds a single input line; pprof or test logs can be long.
	maxLineBytes = 4 * 1024 * 1024
)

// parsedSample is an intermediate result of one benchmark line.
type parsedSample struct {
	// name is the benchmark name with the GOMAXPROCS suffix removed.
	name string
	// sample is the observed repetition.
	sample model.Sample
}

// parsePackageLine recognises `pkg: github.com/x/y` lines, which `go test` prints
// once per package before that package's benchmarks.
func parsePackageLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, pkgPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, pkgPrefix)), true
}

// parseBenchmarkLine parses one benchmark result line. The second return value is
// false for every line that is not a benchmark result, which is the normal case.
func parseBenchmarkLine(line string) (parsedSample, bool) {
	fields := strings.Fields(line)
	if len(fields) < minBenchFields {
		return parsedSample{}, false
	}
	name, ok := stripGoMaxProcs(fields[0])
	if !ok || !strings.HasPrefix(name, benchPrefix) {
		return parsedSample{}, false
	}
	index := 1
	sample := model.Sample{Extra: map[string]float64{}}
	// The iteration count is mandatory in `go test` output, but tolerating its
	// absence costs one branch and makes hand-trimmed fixtures work.
	if iterations, err := strconv.ParseInt(fields[index], 10, 64); err == nil {
		sample.Iterations = iterations
		index++
	}
	metrics := 0
	for index+1 < len(fields) {
		value, err := strconv.ParseFloat(fields[index], 64)
		if err != nil {
			break
		}
		unit := fields[index+1]
		applyMetric(&sample, unit, value)
		metrics++
		index += 2
	}
	if metrics == 0 {
		return parsedSample{}, false
	}
	return parsedSample{name: name, sample: sample}, true
}

// stripGoMaxProcs removes the trailing "-<digits>" GOMAXPROCS suffix from a
// benchmark name. It only strips when the result still starts with "Benchmark", so
// a name that legitimately ends in digits is never mangled.
func stripGoMaxProcs(field string) (string, bool) {
	if !strings.HasPrefix(field, benchPrefix) {
		return "", false
	}
	match := gomaxprocsSuffix.FindStringIndex(field)
	if match == nil {
		return field, true
	}
	candidate := field[:match[0]]
	if !strings.HasPrefix(candidate, benchPrefix) || candidate == benchPrefix {
		return field, true
	}
	return candidate, true
}

// applyMetric stores one "<value> <unit>" pair into the sample, mapping the three
// known units onto their schema fields and everything else into Extra.
func applyMetric(sample *model.Sample, unit string, value float64) {
	switch unit {
	case UnitNsOp:
		sample.NsOp = value
	case UnitBytesOp:
		sample.BytesOp = floatPtr(value)
	case UnitAllocsOp:
		sample.AllocsOp = floatPtr(value)
	default:
		sample.Extra[unit] = value
	}
}

// floatPtr returns a pointer to a copy of value, used for the optional columns.
func floatPtr(value float64) *float64 { return &value }

// benchmarkFor returns the accumulator for a benchmark id, creating it on first
// sight with its workload and variant already resolved.
func (p *Parser) benchmarkFor(index map[string]*model.Benchmark, order *[]string, packageName, name string) *model.Benchmark {
	id := packageName + "." + name
	if existing, ok := index[id]; ok {
		return existing
	}
	workload, variant := p.classify(name)
	benchmark := &model.Benchmark{
		ID:       id,
		Package:  packageName,
		Name:     name,
		Workload: workload,
		Variant:  variant,
		Samples:  []model.Sample{},
	}
	index[id] = benchmark
	*order = append(*order, id)
	return benchmark
}

// classify resolves the workload and variant of a benchmark name. The idiomatic
// sub-benchmark path is handled first; group rules only ever apply to flat names,
// so a repository using sub-benchmarks cannot be affected by a stale rule.
func (p *Parser) classify(name string) (string, string) {
	if strings.Contains(name, "/") {
		return model.WorkloadVariant(name)
	}
	for _, rule := range p.options.GroupRules {
		if !ruleMatches(rule, name) {
			continue
		}
		return rule.Workload, variantFromRule(rule, name)
	}
	return model.WorkloadVariant(name)
}

// ruleMatches reports whether a group rule applies. Both the include and the
// exclude pattern must align with word boundaries, so "DB" does not match inside
// "DBless" and a trailing space can never influence the outcome.
func ruleMatches(rule GroupRule, name string) bool {
	words := splitWords(name)
	if !containsWordSequence(words, rule.Contains) {
		return false
	}
	if strings.TrimSpace(rule.Exclude) == "" {
		return true
	}
	return !containsWordSequence(words, rule.Exclude)
}

// variantFromRule derives the variant of a flat name by removing the workload's own
// words: the remaining suffix is the variant, lower-cased. A name that consists of
// nothing but the workload has no variant and becomes model.DefaultVariant.
func variantFromRule(rule GroupRule, name string) string {
	trimmed := strings.TrimPrefix(name, benchPrefix)
	// The common case: the name starts with the workload exactly as written in the
	// config, so the remainder is the variant in its original spelling ("WithTry"
	// rather than "withtry"), which is what a reader of the report expects.
	if remainder := strings.TrimPrefix(trimmed, rule.Workload); remainder != trimmed {
		if remainder == "" {
			return model.DefaultVariant
		}
		return remainder
	}
	nameWords := splitWords(trimmed)
	workloadWords := splitWords(rule.Workload)
	if len(nameWords) > len(workloadWords) && hasWordPrefix(nameWords, workloadWords) {
		nameWords = nameWords[len(workloadWords):]
	}
	variant := strings.ToLower(strings.Join(nameWords, ""))
	if variant == "" {
		return model.DefaultVariant
	}
	return variant
}

// splitWords splits a CamelCase identifier into lower-cased words, treating
// acronyms as words: "ErrorHandlingCPUWithTry" -> ["error" "handling" "cpu" "with"
// "try"] and "DBQuery" -> ["db" "query"]. Non-alphanumeric characters separate
// words as well. Word-boundary awareness is the whole point: it is what makes
// "WithTry" fail to match "WithoutTry", which a naive strings.Contains would match.
func splitWords(name string) []string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return nil
	}
	words := []string{}
	current := strings.Builder{}
	for i, r := range runes {
		if !isWordRune(r) {
			if current.Len() > 0 {
				words = append(words, strings.ToLower(current.String()))
				current.Reset()
			}
			continue
		}
		if i > 0 && startsNewWord(runes, i) && current.Len() > 0 {
			words = append(words, strings.ToLower(current.String()))
			current.Reset()
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, strings.ToLower(current.String()))
	}
	return words
}

// startsNewWord reports whether the rune at index i begins a new camel-case word.
func startsNewWord(runes []rune, i int) bool {
	previous, current := runes[i-1], runes[i]
	if !isWordRune(previous) {
		return false
	}
	switch {
	case isLowerOrDigit(previous) && isUpper(current):
		return true
	case isUpper(previous) && isUpper(current) && i+1 < len(runes) && isLower(runes[i+1]):
		return true
	default:
		return false
	}
}

// isWordRune reports whether a rune belongs to an identifier word.
func isWordRune(r rune) bool {
	return isLowerOrDigit(r) || isUpper(r) || r >= 0x80
}

// isLowerOrDigit reports whether a rune is a lower-case letter or a digit.
func isLowerOrDigit(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') }

// isUpper reports whether a rune is an upper-case ASCII letter.
func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

// isLower reports whether a rune is a lower-case ASCII letter.
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }

// containsWordSequence reports whether the needle appears in the word sequence as
// a run of complete words, so a match can never start or end inside a word.
func containsWordSequence(words []string, needle string) bool {
	needleWords := splitWords(needle)
	if len(needleWords) == 0 {
		return false
	}
	for i := 0; i+len(needleWords) <= len(words); i++ {
		if hasWordPrefix(words[i:], needleWords) {
			return true
		}
	}
	return false
}

// hasWordPrefix reports whether words starts with the given prefix words.
func hasWordPrefix(words, prefix []string) bool {
	if len(prefix) > len(words) {
		return false
	}
	for i, word := range prefix {
		if words[i] != word {
			return false
		}
	}
	return true
}
