// Package runner. fake - an in-memory Runner used by tests (and by anyone who
// wants to drive benchbook without a Go toolchain). It keeps the test suite honest:
// unit tests never invoke `go test`, so they stay fast and deterministic.
package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------- Types ----------------------------------------- //

// Fake is a scripted Runner.
type Fake struct {
	// mutex guards the recorded calls and scripted answers.
	mutex sync.Mutex
	// answers maps a command prefix (e.g. "git rev-parse HEAD") to its answer.
	answers map[string]FakeAnswer
	// calls records every command in order.
	calls []Command
}

// FakeAnswer is the scripted response for one command prefix.
type FakeAnswer struct {
	// Result is returned when the command matches.
	Result Result
	// Err is returned when the command matches; use *CommandError to simulate a
	// non-zero exit status.
	Err error
}

// ---------------------------------------- Constructor(s) -------------------------------------- //

// NewFake returns an empty Fake. Unknown commands fail loudly, so a test never
// silently depends on a command it did not script.
func NewFake() *Fake { return &Fake{answers: map[string]FakeAnswer{}} }

// ----------------------------------------- Public API ----------------------------------------- //

// Answer scripts a response for every command whose rendered string starts with
// prefix, e.g. `fake.Answer("go test", runner.FakeAnswer{Result: ...})`.
func (f *Fake) Answer(prefix string, answer FakeAnswer) *Fake {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.answers[prefix] = answer
	return f
}

// AnswerStdout is shorthand for scripting a successful command with fixed stdout.
func (f *Fake) AnswerStdout(prefix, stdout string) *Fake {
	return f.Answer(prefix, FakeAnswer{Result: Result{Stdout: []byte(stdout)}})
}

// Calls returns the recorded commands in execution order.
func (f *Fake) Calls() []Command {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	out := make([]Command, len(f.calls))
	copy(out, f.calls)
	return out
}

// CallStrings returns the recorded commands rendered as strings.
func (f *Fake) CallStrings() []string {
	rendered := []string{}
	for _, command := range f.Calls() {
		rendered = append(rendered, command.String())
	}
	return rendered
}

// RanPrefix reports whether any recorded command starts with the given prefix.
func (f *Fake) RanPrefix(prefix string) bool {
	for _, rendered := range f.CallStrings() {
		if strings.HasPrefix(rendered, prefix) {
			return true
		}
	}
	return false
}

// Run implements Runner.
func (f *Fake) Run(_ context.Context, command Command) (Result, error) {
	f.mutex.Lock()
	f.calls = append(f.calls, command)
	rendered := command.String()
	answer, ok := f.match(rendered)
	f.mutex.Unlock()
	if !ok {
		return Result{}, fmt.Errorf("runner fake has no answer for `%s`: script it with Fake.Answer", rendered)
	}
	return answer.Result, answer.Err
}

// match finds the longest scripted prefix of the rendered command. Longest wins so
// a specific answer can override a broader one.
func (f *Fake) match(rendered string) (FakeAnswer, bool) {
	best := ""
	for prefix := range f.answers {
		if strings.HasPrefix(rendered, prefix) && len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return FakeAnswer{}, false
	}
	return f.answers[best], true
}

// ScriptedPrefixes returns the scripted prefixes in ascending order, for debugging
// a failing test.
func (f *Fake) ScriptedPrefixes() []string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	prefixes := make([]string, 0, len(f.answers))
	for prefix := range f.answers {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return prefixes
}

// FailedCommand builds a *CommandError for scripting a failing command, e.g. a git
// repository that does not exist.
func FailedCommand(command Command, exitCode int, stderr string) error {
	return &CommandError{
		Command: command,
		Result:  Result{ExitCode: exitCode, Stderr: []byte(stderr)},
	}
}
