package binding

// resolveScope returns the path a raw scope token names: aliases[raw] if raw
// is a key in the scopes: map, otherwise raw itself — already a literal path.
//
// This is the one place this package looks anything up in scopes:, and it is
// a one-time, mechanical substitution at read time, not a general
// alias-resolution feature: the result always lands in [Binding].Scope, which
// this package's Create and Update never accept an alias key for (see
// binding.go and filestore.go), so a caller of this package can never observe
// or produce a {profile, aliasName} pair, only {profile, path}.
func resolveScope(raw string, aliases map[string]string) string {
	if p, ok := aliases[raw]; ok {
		return p
	}
	return raw
}
