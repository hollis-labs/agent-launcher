package binding

import "sort"

// spliceSpan replaces data[Start:End] with Text.
type spliceSpan struct {
	Start, End int
	Text       string
}

// splice applies spans to data and returns the result. data is never
// modified in place. Every offset in spans must be valid for data and spans
// must not overlap; callers in this package derive them from a single
// [document] scan of the same bytes, which guarantees both.
func splice(data []byte, spans []spliceSpan) []byte {
	ordered := append([]spliceSpan(nil), spans...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Start < ordered[j].Start })

	out := append([]byte(nil), data...)
	// Apply back-to-front (highest Start first): every span not yet applied
	// sits entirely before the one just applied, so its offsets — measured
	// against the original bytes — are still valid for `out` no matter how
	// much the tail's length just changed.
	for i := len(ordered) - 1; i >= 0; i-- {
		s := ordered[i]
		next := make([]byte, 0, len(out)-(s.End-s.Start)+len(s.Text))
		next = append(next, out[:s.Start]...)
		next = append(next, []byte(s.Text)...)
		next = append(next, out[s.End:]...)
		out = next
	}
	return out
}
