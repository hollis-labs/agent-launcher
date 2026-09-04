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
// (not a legitimate reason to skip — see internal/binding's own doc,
// "a missing file is not evidence of anything," for why FileStore itself
// cannot make this same call safely, and why a test can).
package testbundle

import (
	"fmt"
	"os"

	"github.com/hollis-labs/tachyon/internal/binding"
)

// Resolve distinguishes those two conditions for a single bundle root.
//
// It stats bundleRoot itself, never the bindings file inside it, precisely
// so the decision does not depend on which storage format Cairn happens to
// use today: an absent bundleRoot is unambiguously "not on this machine"
// (skip==true, err==nil). A present bundleRoot is real, so from there this
// reads it the same way Tachyon itself does — [binding.Open](bundleRoot)
// .List() — and treats zero bindings coming back, for any reason (the file
// is missing, unreadable, empty, or in a shape this package's narrow scan
// does not recognize), as unambiguously "present but unreadable"
// (skip==false, err!=nil). A test that reaches this point already knows
// which real bindings it expects a real bundle to hold — that expectation
// is exactly the extra context [binding.FileStore] itself does not have,
// and is not allowed to assume, which is why this decision lives in a test
// support package instead of in internal/binding.
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
	if len(list) == 0 {
		return nil, false, fmt.Errorf(
			"testbundle: bundle at %s exists but no bindings could be read from it "+
				"(the bindings file this build of Tachyon reads is missing, empty, or in a "+
				"shape it does not understand — see internal/binding's doc)",
			bundleRoot,
		)
	}
	return list, false, nil
}
