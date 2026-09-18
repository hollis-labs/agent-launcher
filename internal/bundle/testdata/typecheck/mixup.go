// This file must NOT compile. TestProfileAndTemplateAreNotInterchangeable
// builds it and fails if the compiler accepts it.
//
// It lives under testdata/ so the go tool excludes it from ./... — a build
// constraint would exclude it too, but then the build would fail for the wrong
// reason and prove nothing.
package main

import "github.com/hollis-labs/tachyon/internal/bundle"

func wantsProfile(bundle.Profile)     {}
func wantsProfileID(bundle.ProfileID) {}

func main() {
	// A template shares a basename with a profile as readily as role prose
	// used to — templates/lenses/architect.md beside profiles/architect.md —
	// so "the architect template" is ambiguous until the type says which
	// file it means.
	var tpl bundle.Template
	wantsProfile(tpl)
	wantsProfileID(bundle.TemplateID("architect"))
}
