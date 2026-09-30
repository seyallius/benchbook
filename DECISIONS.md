# Decisions

Every entry records a choice the specification left open, and the reason for it.
They are short on purpose: this is the list to read before changing behaviour, not a
diary. Newest sections follow the phases of the build.

## Module and dependencies

**Two dependencies, both boring.** `github.com/BurntSushi/toml` for the configuration
file (small, stable, actively maintained) and `github.com/spf13/cobra` for the command
line, which brings `spf13/pflag` with it. Everything else — the statistics, the
Markdown and SVG rendering, the JSON encoder, the HTTP server — is the standard
library.

*Revised at the author's request.* The original rule was "one dependency, a TOML
library, and flags parsed with the standard `flag` package". Hand-rolling the command
line meant maintaining a usage screen next to the parser, a `parseInterspersed` shim
(the stdlib stops parsing at the first positional argument) and its own error
formatting; cobra removes all three and gives every command real `--help` output. The
cost is one more module to track, which is a trade worth making for a tool whose CLI is
its entire user interface.

**Go 1.27.0, as the language version in `go.mod`.** The toolchain is downloaded and
pinned explicitly so a build is reproducible from the repository alone.

**The command tree is the help screen.** `internal/cli/root.go` builds the cobra tree;
every command declares its own `Use`, `Short`, `Long`, `Example` and flags, and the
help output is generated from that. There is no hand-written usage text to drift out of
date, which is exactly how the previous version managed to describe a flag that had
been renamed.

**Unknown commands, bad flags and missing arguments are usage errors (exit 2).** Cobra
returns plain errors for all three, which would be reported as runtime failures (exit
1) and break the documented exit-code contract. Quieting cobra and re-reporting through
`App.report` keeps one place that decides what an error *means*.

**`help` is written here rather than taken from cobra.** The default help command calls
`os.Exit` on some error paths, which is unacceptable inside a library that tests drive
directly, and its "unknown help topic" leaves the exit code to chance. The replacement
is fifteen lines and behaves like every other command.

**Shell completion is disabled.** `root.CompletionOptions.DisableDefaultCmd = true`:
the tool's interface is documented as a small set of commands, and completion scripts
are a feature nobody asked for. Chosen so `go install
…/cmd/benchbook@latest` works today. Forking means editing one line in `go.mod`;
nothing else refers to the path.

**Version is a constant, not a linker flag.** `cli.Version` is a `const` so a build
from the same source always produces the same bytes, which is what makes the
determinism tests meaningful.

## Configuration

**`Decode` is strict.** Any unknown key in `benchbook.toml` is an error:
`unknown setting(s) in benchbook.toml: <sorted keys>: check the spelling, or see
`benchbook init` for the full example`. A silently ignored typo (`bench.coutn`) is
worse than a startup failure, because it produces plausible-looking wrong numbers.

**Validation stops at the first error.** Reporting every problem at once would be
nicer, but a config that fails validation is edited by hand and rarely has three
independent mistakes; the simple version is easier to keep correct.

**Error messages name the key and a concrete value.** Every validation message
contains the offending key and the value it saw (`stats.alpha = 0: must be > 0`), and
every "how do I fix this" message names a flag or a command.

**Defaults live in `config.Defaults()`, not in the TOML file.** A missing section is
a default, never a zero; `benchbook init` writes a commented example rather than a
complete file, so upgrades never require migrating a config full of default values.

**`[profile]` uses `package`, `top`, `kinds`** with kinds `cpu`, `mem_space`,
`mem_objects` — the same names pprof uses for its sample indices, so the mapping in
`runner.SampleIndexFor` is obvious and no translation table exists.

**`hostname` is empty by default and excluded from the fingerprint.** It is noise that
differs between two shells on the same box.

## Runs, milestones and the store

**`SaveMilestone` requires a caller-set `Seq`.** `NextSeq()` and `SaveMilestone()` are
separate calls so the milestone id can be decided (and validated) before any file
exists, and so `NextSeq` stays a pure question rather than a side effect.

**`NextSeq` is highest existing file + 1.** Deleting the newest milestone frees its
number, which is what a user expects when they delete a mistaken promotion. The
`mNNN-<slug>.json` file name keeps the history readable in a file browser.

**Milestone slugs come from the label**, `m002-strings-join-for-concat.json`; the id
(`m002`) stays stable when a label is later quoted differently in a report.

**An existing milestone is never overwritten.** Promoting identical content prints
`already promoted: mNNN has exactly this content (nothing to do)` and exits 0 — the
command is idempotent so a script may call it twice.

**`content_hash` covers benchmarks and profiles only.** The label and the note are
deliberately excluded, so re-wording a milestone's description in the JSON (or
hashing the same measurements under a new name) is not a content change.

**The working run is a single file, `current.json`.** A run is not history; keeping
the previous working run would create a second, weaker history that competes with the
milestone chain. Every `run` overwrites it.

**Newer `schema_version` is refused, not guessed.** A document written by a newer
benchbook is rejected with an instruction to upgrade. `store.Migrate` exists as a seam
and implements nothing: there is nothing to migrate yet, and inventing migrations for
a version that does not exist is how compatibility bugs are born.

## Collection and provenance

**Git and hardware sit behind interfaces** (`runner.Runner`, `collect.Environment`),
which is what lets the integration tests record a milestone on a fake M2 Pro in
0.02 s without a toolchain.

**Windows hardware comes from `Get-CimInstance`**, never `wmic`, which is deprecated
and absent on newer Windows. Every probe degrades to `unknown` rather than failing the
run: a missing CPU model must not stop a measurement.

**The hardware fingerprint is `sha256(GOOS|GOARCH|CPUModel|cores)`.** Memory size, Go
version, git state and hostname are recorded but excluded: they change for reasons
that do not change what the numbers mean.

**`--no-hw` records `unknown` and a different fingerprint** instead of reusing the last
known one, so a comparison against a real fingerprint is correctly refused.

**A dirty tree is decided by relevance, not by `git status` being non-empty.** Paths
inside `data_dir` and `site_dir` are ignored (they change on every run), and the ignore
prefixes are rebased through `git rev-parse --show-toplevel` so a repository that
lives below the working directory still matches.

**The first three columns of a porcelain line are kept verbatim** when parsing
`git status --porcelain`: left-trimming them destroys the leading space of the path in
` M docs/…` and made every run look dirty.

## Statistics and comparison

**Mann-Whitney U with the exact distribution** below the large-sample threshold.
Benchmarks produce small, non-normal samples with occasional outliers; a rank test
needs no distributional assumption, and the exact path keeps p-values honest at n=6.

**A change needs significance *and* a minimum effect size** (`alpha` and
`min_effect_pct`). A 0.02% difference can be statistically significant and
engineering-irrelevant; the label requires both.

**Fewer than 4 samples per side communicates `insufficient`, never a p-value.** There
is a floor below which a test says nothing, and the report should say that instead of
printing a number nobody should use.

**Percentages are suppressed when the two runs are not comparable.** Difference in
hardware fingerprint or Go version, or one side missing a fingerprint while the other
has one, means `incomparable` with a printed reason and a `--force-incomparable`
override. Two *empty* fingerprints are comparable (both runs genuinely do not know).

**`min_effect_pct <= 0` maps to the default and is rejected by validation.** Zero as a
threshold would silently turn every significant change into a label; the config refuses
it with the key name.

**`Comparison.Geomeans` always has one entry per metric**, with `count: 0` when nothing
could be aggregated, so callers can iterate without nil checks; the tests assert the
per-metric count rather than the slice length.

**`compare.Between(from, to)` is older → newer** and treats `from` as the baseline.
Getting this backwards inverts every sign in the report, so it is stated once, in the
signature.

## Parsing

**The variant keeps its original spelling.** `ErrorHandlingCPUWithTry` yields workload
`ErrorHandlingCPU`, variant `WithTry` (via the `TrimPrefix` fast path), because that is
what the reader of the report sees in the source. The lower-cased fallback is only used
for names that do not literally start with the workload.

**Group-rule matching is word-boundary aware.** `WithTry` must not match `WithoutTry`;
`splitWords` treats acronyms as words (`DBQuery` → `db`, `query`), at the cost of *not*
splitting letter→digit runs, so `Parse2Items` stays one word. A `strings.Contains`
implementation is simpler and wrong in exactly the cases that matter.

**pprof `Meta` is kept verbatim**, including `Build ID:` and `Showing top N nodes out
of M`. The tables are quoted in a four-backtick fence precisely so that a reader can
check benchbook's arithmetic against the tool that produced the numbers.

**Units are normalised to base units** (ns, bytes, objects, or the metric's own unit);
`kB` means 1024, `ms` means 1e6 ns. Formatting decides how to print, parsing decides
what is true.

## Rendering and the site

**Profiles belong to the run they were captured from.** `benchbook profile` attaches
the tables to the working run, and `milestone` copies them into the history, so the
order is `run` → `profile` → `milestone`. A later `run` replaces the working run
including its profiles: a profile of yesterday's binary is not a profile of today's.
The alternative (merging old profiles into a new run) would attach evidence to
measurements it does not describe.

**`EscapeCell` leaves backticks alone.** It escapes what actually breaks a table cell
(`|`, newlines, angle brackets) and nothing else, because the table escapes every cell
*after* `code()` has wrapped a value in a code span. Escaping the fence there turned
every milestone page's `\`abc123\`` into a literal backslash-backtick; the regression
test asserts on the composed table, not just on the escaper.

**Structured JSON is the source of truth; pages are derived.** Renderers are pure
functions from the model to a string, which is what makes the golden tests cheap and
the byte-identical determinism guarantee testable.

**Every generated Markdown file starts with `<!-- generated by benchbook: DO NOT
EDIT -->`.** It is the only mechanism that reliably stops a well-meaning human from
editing a file that the next build will overwrite.

**Timeline charts are inline SVG generated in Go.** No chart library, no canvas, no
JavaScript needed to read history, and — because the generator is deterministic — the
same history always produces the same bytes.

**`bb-chart-current` is a distinct class on the segment leading to the unpromoted run**
(dotted line and larger dot). A working run that has not been promoted must be visibly
provisional on a page that otherwise shows promoted milestones.

**Hardware changes are drawn as a vertical break** in the timeline chart, matching the
"not directly comparable" rule instead of leaving the reader to notice a fingerprint
change in a table.

**The "Hardware & toolchain" block is a `##` section**, matching every other top-level
section of a generated page, so the page outline is consistent for anchors and for
anyone reading the raw Markdown.

**`"1 alloc"` is singular.** The allocation counter is pluralised like a noun
(`allocsSuffix`); "1 allocs" is the kind of detail that makes a generated report feel
machine-written.

**The browser comparer is checked against the same golden vectors as Go**
(`testdata/utest_vectors.json`, `scripts/test-compare.mjs`). Duplicated statistics are
a liability; duplicated statistics with a shared fixture are merely a maintenance cost.

**The site shell is embedded with `go:embed` and vendored, never fetched.** `marked`
15.0.7 and `highlight.js` 11.11.1 (common bundle + Go grammar + two themes) are pinned
by SHA-256 in `internal/site/shell/VENDOR.md` and installed by `scripts/vendor.sh`,
which refuses a checksum mismatch. A generated site must work offline and on GitHub
Pages without a CDN.

**Only files benchbook owns are written.** Authored pages under `docs/content/` are left
alone; a missing one is a warning in the navigation, not an error, because a broken
link must not stop a build. `init` prints the justfile snippet instead of editing the
project's `justfile` — silently rewriting someone's build file is rude.

**`manifest.json` exports the comparison thresholds.** The browser needs `alpha`,
`min_effect_pct` and `min_samples`; exporting the same numbers the CLI uses is what
keeps the page and the terminal from disagreeing.

## CLI

**Exit codes 0/1/2/3 are part of the contract**, with 3 reserved for refusals, so a
script can tell "you must not" from "it broke" without parsing English. Commands no
longer return a code alongside an error: `App.report` derives the code from the error's
type, which is the only place that decision needs to be made.

**`--no-color` and `NO_COLOR` are honoured, and colour is only ever emitted for a TTY.**
Piping benchbook into a file or a CI log produces clean, diffable text.

**`--quick` is a real feature, not a shortcut around the rules.** It records a
one-sample run marked `indicative only`; the promotion rules then refuse it unless
`--force` is given, and a forced milestone keeps a ⚠ badge forever.

**Tests never run `go test -bench`.** `runner.Fake` answers every external command, so
unit and integration tests stay fast, deterministic and CI-friendly — and the CI
workflow deliberately runs no benchmarks at all, because numbers from a shared runner
are noise.
