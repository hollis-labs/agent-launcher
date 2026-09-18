package bundle_test

import (
	"testing"

	"github.com/hollis-labs/tachyon/internal/bundle"
)

func TestScanHeader(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bundle.Header
	}{{
		name: "no frontmatter",
		in:   "# Just a markdown document\n\nid: not-frontmatter\n",
		want: bundle.Header{},
	}, {
		name: "empty file",
		in:   "",
		want: bundle.Header{},
	}, {
		name: "the four fields",
		in: "---\nid: architect\nextends: base\nname: Architect\n" +
			"description: Decides structure and boundaries.\nprovider: claude\n---\n\nbody\n",
		want: bundle.Header{Present: true, ID: "architect", Extends: "base", Name: "Architect",
			Description: "Decides structure and boundaries."},
	}, {
		name: "comments and blank lines inside the block",
		in:   "---\n# a comment\n\nid: base\n\n# another\nabstract: true\nname: Floor\n---\n",
		want: bundle.Header{Present: true, ID: "base", Name: "Floor", Abstract: true},
	}, {
		// The reason the scan refuses to descend: spec.slots has a name too,
		// and it is not the profile's name.
		name: "nested keys are not read",
		in:   "---\nid: base\nspec:\n  name: not-the-profile-name\n  slots:\n    - name: role\n      id: nope\n---\n",
		want: bundle.Header{Present: true, ID: "base"},
	}, {
		name: "quoted scalars",
		in:   "---\nid: 'quoted'\nname: \"A name: with a colon\"\nextends: \"base\"  # trailing comment\n---\n",
		want: bundle.Header{Present: true, ID: "quoted", Name: "A name: with a colon", Extends: "base"},
	}, {
		name: "single-quote escape",
		in:   "---\nname: 'it''s fine'\n---\n",
		want: bundle.Header{Present: true, Name: "it's fine"},
	}, {
		name: "trailing comment on a plain scalar",
		in:   "---\ndescription: plain value # and a comment\n---\n",
		want: bundle.Header{Present: true, Description: "plain value"},
	}, {
		name: "a hash inside a word is not a comment",
		in:   "---\nname: issue#42\n---\n",
		want: bundle.Header{Present: true, Name: "issue#42"},
	}, {
		name: "null spellings read as absent",
		in:   "---\nid: x\nextends: null\nname: ~\ndescription:\n---\n",
		want: bundle.Header{Present: true, ID: "x"},
	}, {
		name: "block scalars and flow collections are not read",
		in:   "---\nid: x\ndescription: |\n  folded prose\n  more prose\nextends: [a, b]\nname: {a: b}\n---\n",
		want: bundle.Header{Present: true, ID: "x"},
	}, {
		name: "first occurrence of a key wins",
		in:   "---\nid: first\nid: second\n---\n",
		want: bundle.Header{Present: true, ID: "first"},
	}, {
		name: "key without a space is not a mapping entry",
		in:   "---\nid:architect\nname: ok\n---\n",
		want: bundle.Header{Present: true, Name: "ok"},
	}, {
		name: "keys are case sensitive",
		in:   "---\nID: shouty\nName: shouty\n---\n",
		want: bundle.Header{Present: true},
	}, {
		name: "scanning stops at the closing marker",
		in:   "---\nid: real\n---\nid: in-the-body\nname: in-the-body\n",
		want: bundle.Header{Present: true, ID: "real"},
	}, {
		name: "an explicit document end also closes the block",
		in:   "---\nid: real\n...\nname: in-the-body\n",
		want: bundle.Header{Present: true, ID: "real"},
	}, {
		// Not an error: shape and existence only. An unterminated block still
		// yields what it plainly says.
		name: "unterminated block",
		in:   "---\nid: unterminated\nname: Still Read\n",
		want: bundle.Header{Present: true, ID: "unterminated", Name: "Still Read"},
	}, {
		name: "CRLF line endings",
		in:   "---\r\nid: crlf\r\nname: CRLF\r\n---\r\n",
		want: bundle.Header{Present: true, ID: "crlf", Name: "CRLF"},
	}, {
		name: "leading BOM",
		in:   "\xEF\xBB\xBF---\nid: bom\n---\n",
		want: bundle.Header{Present: true, ID: "bom"},
	}, {
		name: "a marker that is not on the first line is not frontmatter",
		in:   "\n---\nid: late\n---\n",
		want: bundle.Header{},
	}, {
		name: "a sequence item is not a top-level key",
		in:   "---\n- id: item\nname: real\n---\n",
		want: bundle.Header{Present: true, Name: "real"},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bundle.ScanHeader([]byte(tc.in)); got != tc.want {
				t.Fatalf("ScanHeader = %+v; want %+v", got, tc.want)
			}
		})
	}
}

// TestScanHeaderAbstract pins the one key added for the launcher: a palette
// must be able to keep an abstract profile out of a list of things to boot,
// because cairn refuses one and the refusal is not something a person can
// act on.
//
// Anything but a recognized true reads as false, including an absent key.
// That is the safe direction: a value this scanner does not recognize leaves
// the profile bootable, and cairn refuses it with its own diagnostic either
// way — where the opposite mistake hides a bootable profile from the palette
// with nothing said.
func TestScanHeaderAbstract(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"---\nid: base\nabstract: true\n---\n", true},
		{"---\nid: base\nabstract: yes\n---\n", true},
		{"---\nid: base\nabstract: false\n---\n", false},
		{"---\nid: base\n---\n", false},
		{"---\nid: base\nabstract: \"true\"\n---\n", true},
		{"---\nid: base\nabstract: maybe\n---\n", false},
	} {
		if got := bundle.ScanHeader([]byte(tc.in)).Abstract; got != tc.want {
			t.Errorf("ScanHeader(%q).Abstract = %v; want %v", tc.in, got, tc.want)
		}
	}
}
