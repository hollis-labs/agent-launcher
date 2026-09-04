package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// guardTimeout bounds a single lsof invocation ([probe]'s call to a
// [LsofRunner]). +D walks a candidate's tree recursively; this is what
// keeps a pathological or hung filesystem from wedging [Sweep] forever
// instead of reporting a clean "invocation failed" for that one candidate
// (or, for the positive control, for the whole run — see [Sweep]'s doc).
const guardTimeout = 5 * time.Second

// LsofRunner is the seam [Sweep]'s guard shells out through, matching
// [Runner]'s own shape in invoke.go: capture stdout and stderr separately,
// let a test substitute a fake instead of a real subprocess. args is
// exactly what follows the binary name — [Sweep] always calls it with
// exactly ["-a", "-d", "cwd", "+D", <candidate-or-control-target>], never
// anything else, and never through a shell.
type LsofRunner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// ExecLsofRunner returns a [LsofRunner] that runs lsofPath directly via
// exec.CommandContext — never through a shell, and never through an
// external wrapper such as `timeout`. Three independent people building
// toward CW-20260903-0019 lost time to the same trap: `timeout` is not
// installed on this machine, a shell silently swallowed "command not
// found", and the resulting empty output read exactly like a clean "no
// holders" answer. See this file's package-level doc below for the full
// argument against wrapping lsof in anything.
//
// An empty lsofPath runs the bare command name "lsof", letting exec's own
// PATH lookup at process-start time resolve it. A caller that has already
// resolved an absolute path — exec.LookPath("lsof"), the same pattern
// cmd/tachyon/main.go already uses for cairn — should pass it here
// instead: a GUI app launched from Finder inherits a minimal launchd PATH,
// and lsof living outside it is not hypothetical to guard against cheaply.
//
// A binary that cannot be found or started is not a special case here: it
// surfaces as an ordinary error from the returned function's third result,
// which [probe] already classifies correctly (an error that is not an
// *exec.ExitError means the invocation itself failed, not that lsof ran
// and reported something) — see [Sweep]'s doc for why that distinction,
// and not the exit code, is what "fail closed" means for this command.
func ExecLsofRunner(lsofPath string) LsofRunner {
	path := lsofPath
	if path == "" {
		path = "lsof"
	}
	return func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
}

// SkipReason is one candidate [Sweep] left alone, and why. Every candidate
// Sweep considers ends up in exactly one of [Report.Swept] or
// [Report.Skipped] — never silently absent from the report.
type SkipReason struct {
	// Path is the candidate's full path (root/key/.prev-<timestamp>).
	Path string `json:"path"`
	// Reason is a human-readable explanation: a holder count, an
	// invocation failure, or (when [Report.GuardOK] is false) that the
	// guard itself could not be trusted this run.
	Reason string `json:"reason"`
}

// Report is what one [Sweep] run found. GuardOK is the field a caller must
// check before treating an empty Swept as reassuring: see [Sweep]'s doc
// for why "nothing was live" and "the guard is broken" produce the exact
// same lsof output on this host, and why GuardOK — not the exit code, not
// an empty Skipped — is the only thing that tells them apart.
type Report struct {
	// Swept is every candidate path Sweep actually removed.
	Swept []string `json:"swept"`
	// Skipped is every candidate path Sweep left alone, and why. When
	// GuardOK is false, every candidate Sweep found is here, each with a
	// reason naming GuardDetail — nothing is silently omitted just
	// because the guard could not run.
	Skipped []SkipReason `json:"skipped"`
	// GuardOK reports whether this run's positive control — the identical
	// guard call, run against a directory known to be live while Sweep
	// runs — found at least one holder. When false, nothing was deleted
	// regardless of what any individual candidate's own guard call
	// reported, because a broken guard's "zero holders" answer for a real
	// candidate is indistinguishable from a working guard's.
	GuardOK bool `json:"guardOK"`
	// GuardDetail explains GuardOK: empty when true, otherwise why the
	// guard could not be trusted this run (lsof missing or unusable, an
	// invocation error, output that didn't parse, or the control call
	// itself finding zero holders of a directory known to be live).
	GuardDetail string `json:"guardDetail"`
}

// Sweep enumerates every root/<key>/.prev-* directory across every key
// under root — every binding's own .prev-* history, not just one — and
// removes the ones the liveness guard reports as genuinely free, provided
// the guard's own positive control succeeds for this run. It never
// touches [CurrentSegment] ("current"), never touches anything outside
// root, and never removes anything not named with the [PrevPrefix]
// convention [Prepare] produces.
//
// runner is required; production callers pass [ExecLsofRunner], tests pass
// a fake. Sweep calls runner with exactly ["-a", "-d", "cwd", "+D",
// target] for both the positive control and every candidate — the same
// call, the same form, every time, which is what makes the control a
// meaningful proof of the mechanism rather than a proof of some other code
// path.
//
// # Why row count, not exit code, is the signal
//
// The guard this function runs is `lsof -a -d cwd +D <target>`. Measured
// directly on this host (CW-20260903-0019, comment 2633, reproduced
// independently while implementing this function — see the commit message
// for the replay): +D's recursive directory walk can make lsof exit
// non-zero — man lsof: exit 1 means "any error was detected" — regardless
// of whether it found a holder. A held directory can exit 0 or 1
// depending on incidental tree shape; a free directory typically exits 1.
// Exit code alone cannot distinguish "no holders" from "found a holder but
// the recursive walk also tripped an unrelated warning" from "lsof itself
// failed to run at all" — the last of which is the one case that actually
// must block a deletion.
//
// So this function does not treat a non-zero exit as failure by itself.
// It parses stdout: a header line ("COMMAND ...") followed by zero or more
// data rows. Zero data rows is treated as "no holders" — but only when
// combined with clean invocation sanity (no error that isn't an
// *exec.ExitError, empty stderr, output that parses as lsof's format) AND
// this run's positive control. A non-*exec.ExitError (lsof not found,
// couldn't be started, context deadline exceeded), non-empty stderr, or
// unparseable output all still fail closed — those are the "invocation
// went wrong" cases exit code and stderr remain useful for.
//
// # The positive control
//
// A "no holders" answer and a broken guard produce byte-identical lsof
// output — nothing. Nothing about a single candidate's own result can
// tell those apart. So before trusting any candidate's "zero rows" answer,
// Sweep runs the identical guard call against the sweep process's own
// working directory — definitionally live for as long as Sweep is
// running — and requires it to find at least one row. If it does not (or
// itself fails invocation sanity), Sweep treats the guard as unusable for
// this entire run: nothing is deleted, every candidate found is reported
// in Skipped, and GuardDetail says why. This runs fresh on every call, not
// once and cached, so a mechanism that breaks mid-run (lsof becomes
// unavailable, permissions change) is caught rather than assumed away.
//
// # Why not exact-path
//
// lsof's exact-path form (`-- <path>`, no +D) does exit cleanly on this
// host, which makes it tempting. It was measured and rejected: a boot
// directory always contains .claude/ (where T10's --settings file lives),
// so a session that has cd'd into that subdirectory has a cwd that is a
// real subdirectory of the candidate, not the candidate itself —
// exact-path misses it completely. A clean exit code is not worth a wrong
// answer; see this package's doc comment and CW-20260903-0019's Tesseract
// investigation (lsof_cwd_guard_2026_09_03) for the fuller history of why
// this specific simplification keeps recurring and keeps being wrong.
func Sweep(ctx context.Context, root string, runner LsofRunner) (Report, error) {
	cands, err := prevCandidates(root)
	if err != nil {
		return Report{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return guardUnavailable(cands, fmt.Sprintf(
			"resolving the sweep process's own working directory for the positive control: %v", err)), nil
	}

	control := probe(ctx, runner, cwd)
	switch {
	case !control.sane:
		return guardUnavailable(cands, fmt.Sprintf(
			"positive control against %s: %s", cwd, control.detail)), nil
	case control.rows < 1:
		return guardUnavailable(cands, fmt.Sprintf(
			"positive control against %s (this process's own cwd, which is definitionally live right now) found zero holders — the lsof guard cannot be trusted this run",
			cwd)), nil
	}

	report := Report{GuardOK: true}
	for _, c := range cands {
		outcome := probe(ctx, runner, c)
		switch {
		case !outcome.sane:
			report.Skipped = append(report.Skipped, SkipReason{
				Path: c, Reason: "guard invocation failed: " + outcome.detail,
			})
		case outcome.rows > 0:
			report.Skipped = append(report.Skipped, SkipReason{
				Path:   c,
				Reason: fmt.Sprintf("still held open as a cwd by %d process(es)", outcome.rows),
			})
		default:
			if rmErr := os.RemoveAll(c); rmErr != nil {
				report.Skipped = append(report.Skipped, SkipReason{
					Path: c, Reason: "remove failed: " + rmErr.Error(),
				})
				continue
			}
			report.Swept = append(report.Swept, c)
		}
	}
	return report, nil
}

// guardUnavailable builds the Report Sweep returns when the positive
// control did not succeed: GuardOK false, every found candidate present in
// Skipped with detail as its reason, and nothing removed. Candidates are
// still enumerated and named individually here — even though none of them
// were probed — so a candidate sitting unswept forever has a visible
// reason attached to it rather than simply not appearing anywhere.
func guardUnavailable(cands []string, detail string) Report {
	r := Report{GuardOK: false, GuardDetail: detail}
	for _, c := range cands {
		r.Skipped = append(r.Skipped, SkipReason{
			Path: c, Reason: "guard unavailable this run: " + detail,
		})
	}
	return r
}

// prevCandidates lists every root/<key>/.prev-* directory across every key
// directly under root. A key is any directory directly under root — a
// stray file sitting in root is silently not a key and contributes no
// candidates. Within a key, only entries that are themselves directories
// and whose name has the [PrevPrefix] prefix are candidates: a file
// merely named like a .prev-* directory, a symlink (os.ReadDir's IsDir
// reflects the entry's own type, never a symlink target's), and
// [CurrentSegment] itself are all excluded by construction, not by a
// separate check. A missing root is not an error — nothing has been
// planted yet — and returns (nil, nil).
func prevCandidates(root string) ([]string, error) {
	keys, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("boot: sweep: reading boot root %s: %w", root, err)
	}

	var out []string
	for _, key := range keys {
		if !key.IsDir() {
			continue
		}
		keyDir := filepath.Join(root, key.Name())
		entries, err := os.ReadDir(keyDir)
		if err != nil {
			// One unreadable key directory must not stop every other key's
			// candidates from being found, and — failing closed by
			// construction, not by choice — it also means nothing under it
			// is ever reported as a candidate, so nothing under it is ever a
			// deletion risk either.
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if !strings.HasPrefix(e.Name(), PrevPrefix) {
				continue
			}
			out = append(out, filepath.Join(keyDir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// probeOutcome is one guard call's result: how many holder rows lsof
// reported, and whether the invocation itself was sane enough to trust
// that count at all.
type probeOutcome struct {
	rows   int
	sane   bool
	detail string
}

// probe runs `lsof -a -d cwd +D target` through runner, bounded by
// [guardTimeout], and classifies the result. See [Sweep]'s doc for the
// full argument; the short version: an error that is not an
// *exec.ExitError (lsof could not be started, PATH lookup failed, the
// timeout fired) means the invocation itself is untrustworthy — sane is
// false. An *exec.ExitError alone means nothing here; lsof's own non-zero
// exit is expected and uninformative on this host, whether or not it
// found holders. Non-empty stderr or output that does not parse as
// lsof's format also mean sane is false. Otherwise sane is true and rows
// is the number of data lines after lsof's header — zero for "no
// holders found", which is the only thing "no holders" ever looks like.
func probe(ctx context.Context, runner LsofRunner, target string) probeOutcome {
	callCtx, cancel := context.WithTimeout(ctx, guardTimeout)
	defer cancel()

	stdout, stderr, err := runner(callCtx, "-a", "-d", "cwd", "+D", target)

	if callCtx.Err() != nil {
		return probeOutcome{sane: false, detail: fmt.Sprintf(
			"lsof timed out or was canceled probing %s: %v", target, callCtx.Err())}
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return probeOutcome{sane: false, detail: fmt.Sprintf(
				"running lsof against %s: %v", target, err)}
		}
		// An *exec.ExitError: lsof ran and exited non-zero. On this host
		// that is lsof's ordinary shape for both "no holders" and "found a
		// holder but +D's recursive walk also tripped an unrelated
		// warning" under +D — the exit code carries no signal here. Fall
		// through to stderr and stdout, which do.
	}
	if len(stderr) > 0 {
		return probeOutcome{sane: false, detail: fmt.Sprintf(
			"lsof wrote to stderr probing %s: %s", target, strings.TrimSpace(string(stderr)))}
	}
	rows, ok := parseLsofRows(stdout)
	if !ok {
		return probeOutcome{sane: false, detail: fmt.Sprintf(
			"lsof output probing %s did not parse as expected: %q", target, string(stdout))}
	}
	return probeOutcome{rows: rows, sane: true}
}

// parseLsofRows parses `lsof -a -d cwd +D` stdout: empty output is zero
// rows (lsof's ordinary "found nothing" shape on this host — see [Sweep]'s
// doc), a header line beginning with the literal field "COMMAND" followed
// by zero or more data lines is that many rows, and anything else — output
// that is non-empty but does not start with lsof's own header — is
// unparseable, which [probe] treats as an invocation-sanity failure rather
// than guessing at a row count.
func parseLsofRows(stdout []byte) (rows int, ok bool) {
	text := strings.TrimSpace(string(stdout))
	if text == "" {
		return 0, true
	}
	lines := strings.Split(text, "\n")
	fields := strings.Fields(lines[0])
	if len(fields) == 0 || fields[0] != "COMMAND" {
		return 0, false
	}
	return len(lines) - 1, true
}
