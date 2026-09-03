// This file must NOT compile. TestProfileAndRoleProseAreNotInterchangeable
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
	// All eight role prose files share a basename with a profile, so "the
	// architect role" is ambiguous until the type says which file it means.
	var prose bundle.RoleProse
	wantsProfile(prose)
	wantsProfileID(bundle.RoleProseID("architect"))
}
