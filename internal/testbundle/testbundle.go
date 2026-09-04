// Package testbundle gives Tachyon's own tests one shared place to decide
// whether pointing at a real, on-disk bundle should skip or fail. See
// [Resolve].
//
// It exists because internal/boot/reconcile_test.go,
// cmd/tachyon/integration_test.go and, prior to CW-20260904-0003 (T24),
// internal/launch/launch_test.go's own fixture writer each grew a slightly
// different copy of the same decision, and every one of those copies
// collapsed two different conditions into one t.Skipf: "no bundle on this
// machine at all" (a legitimate reason to skip) and "the bundle is right
// there but this build of Tachyon cannot read a single binding out of it"
// (not a legitimate reason to skip).
//
// # A simpler Resolve since CW-20260904-0002 (T23)
//
// Before T23, [binding.FileStore] itself could not tell "never had
// bindings" apart from "bindings moved out from under us": a missing
// bindings file behaved exactly like an empty one, on purpose (see
// internal/binding's own doc, "a missing file is not evidence of
// anything," as it read at the time). This package's own [Resolve] worked
// around that by treating *zero bindings coming back, for any reason* as
// "present but unreadable" — a heuristic that lived here, not in
// internal/binding, precisely because a test already knows which real
// bindings a specific bundle root ought to contain, which is exactly the
// extra context [binding.FileStore] itself did not have and was not
// allowed to assume.
//
// T23 migrated internal/binding's read path to a bindings/ directory, and
// with it, production [binding.FileStore.List] now genuinely distinguishes
// "bindings/ is missing" ([binding.ErrBindingsDirMissing]), "bindings/ is
// present but unreadable" (a different, non-nil error), and "bindings/ is
// present and genuinely empty" ((nil, nil) with a zero-length slice) — see
// internal/binding's doc, "the directory's own existence is real
// information." This package's own "zero results means unreadable"
// heuristic is retired as a result: [Resolve] below just propagates
// whatever [binding.FileStore.List] reports, because the disambiguation it
// used to have to fake now genuinely lives at internal/binding's own
// boundary. The one thing this package still adds on top is the "bundle
// root does not exist on this machine at all" skip — internal/binding has
// no opinion about that; it only ever looks inside a root it is handed.
package testbundle

import (
	"fmt"
	"os"

	"github.com/hollis-labs/tachyon/internal/binding"
)

// Resolve distinguishes "no bundle on this machine" from "a real bundle
// whose bindings this build of Tachyon cannot read" for a single bundle
// root.
//
// It stats bundleRoot itself, never bindings/ inside it, precisely so the
// "not on this machine" decision does not depend on which storage format
// Cairn happens to use today: an absent bundleRoot is unambiguously "not on
// this machine" (skip==true, err==nil). A present bundleRoot is real, so
// from there this reads it the same way Tachyon itself does —
// [binding.Open](bundleRoot).List() — and returns whatever that reports:
// its bindings on success, or its error (skip==false, err!=nil) for
// anything else, including bindings/ being absent, wrong-shaped, or
// carrying a file this package's narrow scan cannot parse. See this
// package's own doc for why that error now comes from [binding.FileStore]
// itself rather than from a heuristic here.
func Resolve(bundleRoot string) (bindings []binding.Binding, skip bool, err error) {
	if _, statErr := os.Stat(bundleRoot); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("testbundle: statting %s: %w", bundleRoot, statErr)
	}

	list, err := binding.Open(bundleRoot).List()
	if err != nil {
		return nil, false, fmt.Errorf("testbundle: reading bindings under %s: %w", bundleRoot, err)
	}
	return list, false, nil
}
