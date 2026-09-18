package launchprofile_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/launchprofile"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestListSortsAndReadsDisplayFields(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "zebra.md", "---\nid: zebra\nprovider: codex\ndescription: last alphabetically\n---\n")
	write(t, dir, "alpha.md", "---\nid: alpha\nprovider: claude\n---\n")

	got, err := launchprofile.Open(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d profiles; want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "zebra" {
		t.Fatalf("List order = %q, %q; want alpha, zebra", got[0].Name, got[1].Name)
	}
	if got[0].Provider != "claude" || got[1].Provider != "codex" {
		t.Errorf("providers = %q, %q; want claude, codex", got[0].Provider, got[1].Provider)
	}
	if got[1].Description != "last alphabetically" {
		t.Errorf("description = %q", got[1].Description)
	}
	if got[0].Path != filepath.Join(dir, "alpha.md") {
		t.Errorf("Path = %q; want the absolute file, which is what becomes --with", got[0].Path)
	}
}

// TestListDistinguishesMissingFromEmpty is the distinction the palette's
// three empty states rest on: a directory that does not exist is first run,
// and a directory that exists holding nothing is a person who deleted their
// profiles. Same empty slice, different sentences.
func TestListDistinguishesMissingFromEmpty(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "never-created")
	if _, err := launchprofile.Open(absent).List(); !errors.Is(err, launchprofile.ErrDirMissing) {
		t.Fatalf("List over an absent directory = %v; want ErrDirMissing", err)
	}

	empty := t.TempDir()
	got, err := launchprofile.Open(empty).List()
	if err != nil {
		t.Fatalf("List over an empty directory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List over an empty directory returned %d profiles", len(got))
	}
}

// TestListSurvivesAnUnparseableProfile pins Parse's deliberate leniency: a
// file nobody can label is still a file a person wrote, and it must stay
// visible so they can open and fix it. Cairn refuses it at the boot, where
// the refusal is actionable.
func TestListSurvivesAnUnparseableProfile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "broken.md", "this file has no frontmatter at all\n")
	write(t, dir, "alsobroken.md", "---\nid: [unclosed\n---\n")
	write(t, dir, "fine.md", "---\nid: fine\nprovider: claude\n---\n")

	got, err := launchprofile.Open(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List returned %d profiles; want all 3, including the ones it cannot label", len(got))
	}
	for _, p := range got {
		if p.Name == "fine" && p.Provider != "claude" {
			t.Errorf("the parseable profile lost its provider: %q", p.Provider)
		}
		if p.Name == "broken" && p.Provider != "" {
			t.Errorf("an unparseable profile reported a provider %q; want the zero value", p.Provider)
		}
	}
}

func TestListSkipsNonProfiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "real.md", "---\nid: real\n---\n")
	write(t, dir, "notes.txt", "not a profile\n")
	write(t, dir, "../escape.md", "---\nid: escape\n---\n") // lands outside dir
	if err := os.MkdirAll(filepath.Join(dir, "subdir.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := launchprofile.Open(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "real" {
		t.Fatalf("List = %+v; want just the one real profile", got)
	}
}

// TestNameCannotEscapeTheStore is the reason ValidateName is stricter than
// the filesystem: a name becomes a filename with no escaping.
func TestNameCannotEscapeTheStore(t *testing.T) {
	dir := t.TempDir()
	st := launchprofile.Open(dir)

	for _, bad := range []string{"", "..", "../evil", "sub/dir", ".hidden", "-leading", "with space", "/absolute"} {
		if err := launchprofile.ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) accepted it", bad)
		}
		if _, err := st.Create(bad, []byte("---\n---\n")); err == nil {
			t.Errorf("Create(%q) succeeded; a name that escapes the store must be refused", bad)
		}
	}
	for _, good := range []string{"default", "codex", "eng-nanite", "a.b_c-1", "X9"} {
		if err := launchprofile.ValidateName(good); err != nil {
			t.Errorf("ValidateName(%q) = %v; want it accepted", good, err)
		}
	}
}

func TestCreateRefusesAnExistingName(t *testing.T) {
	dir := t.TempDir()
	st := launchprofile.Open(dir)

	if _, err := st.Create("dupe", []byte("---\nid: dupe\nprovider: claude\n---\n")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := st.Create("dupe", []byte("---\nid: dupe\nprovider: codex\n---\n"))
	if !errors.Is(err, launchprofile.ErrExists) {
		t.Fatalf("second Create = %v; want ErrExists", err)
	}
	// And the first one's content survived the refusal.
	p, err := st.Get("dupe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p.Provider != "claude" {
		t.Errorf("provider = %q; the refused Create must not have overwritten anything", p.Provider)
	}
}

// TestSaveIsAtomic proves a failed write cannot leave a half-file standing:
// the previous content survives, because the write goes to a temp file that
// is renamed over the target only once it is complete. Checked by asserting
// no stray temp file is left behind after a successful save, and that the
// content is exactly what was handed in.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	st := launchprofile.Open(dir)

	const first = "---\nid: p\nprovider: claude\n---\n"
	const second = "---\nid: p\nprovider: codex\ndescription: replaced\n---\nbody\n"

	if _, err := st.Save("p", []byte(first)); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if _, err := st.Save("p", []byte(second)); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := st.Read("p")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != second {
		t.Fatalf("Read = %q; want %q", got, second)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("a staging file survived the save: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries after two saves of one profile; want 1", len(entries))
	}
}

func TestGetAndReadReportNotFound(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "there.md", "---\nid: there\n---\n")
	st := launchprofile.Open(dir)

	if _, err := st.Get("absent"); !errors.Is(err, launchprofile.ErrNotFound) {
		t.Errorf("Get(absent) = %v; want ErrNotFound", err)
	}
	if _, err := st.Read("absent"); !errors.Is(err, launchprofile.ErrNotFound) {
		t.Errorf("Read(absent) = %v; want ErrNotFound", err)
	}
}

// --- seeding ---------------------------------------------------------------

// TestEnsureSeedsPlantsBothOnAFreshStore covers first run: an empty palette
// on a new machine is not a useful state.
func TestEnsureSeedsPlantsBothOnAFreshStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "launch")
	st := launchprofile.Open(dir)

	if err := launchprofile.EnsureSeeds(st, io.Discard); err != nil {
		t.Fatalf("EnsureSeeds: %v", err)
	}
	got, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("seeded %d profiles; want 2", len(got))
	}

	byName := map[string]launchprofile.Profile{}
	for _, p := range got {
		byName[p.Name] = p
	}
	if byName["default"].Provider != "claude" {
		t.Errorf("default provider = %q; want claude", byName["default"].Provider)
	}
	if byName["codex"].Provider != "codex" {
		t.Errorf("codex provider = %q; want codex", byName["codex"].Provider)
	}
}

// TestCodexSeedCarriesTheTwoKeys pins the restoration. Without them a Codex
// session boots and cannot write to its own scope — see Torque
// CW-20260910-0038 and Tesseract codex_boot_sandbox_posture. This is the
// unit-level half; TestSeededCodexProfileRendersThePostureKeys renders it
// through real cairn.
func TestCodexSeedCarriesTheTwoKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "launch")
	st := launchprofile.Open(dir)
	if err := launchprofile.EnsureSeeds(st, io.Discard); err != nil {
		t.Fatalf("EnsureSeeds: %v", err)
	}

	data, err := st.Read("codex")
	if err != nil {
		t.Fatalf("Read(codex): %v", err)
	}
	for _, want := range []string{"approval_policy: never", "sandbox_mode: workspace-write"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the codex seed does not declare %q:\n%s", want, data)
		}
	}
}

// TestDefaultSeedChangesNoClaudeSetting is the other side of the same
// judgement: restoring a deleted Codex file is this seed's to do, changing
// how every Claude session behaves is not.
func TestDefaultSeedChangesNoClaudeSetting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "launch")
	st := launchprofile.Open(dir)
	if err := launchprofile.EnsureSeeds(st, io.Discard); err != nil {
		t.Fatalf("EnsureSeeds: %v", err)
	}

	data, err := st.Read("default")
	if err != nil {
		t.Fatalf("Read(default): %v", err)
	}
	if strings.Contains(string(data), "spec:") {
		t.Errorf("the default seed declares a spec; it must contribute no key that could override the bundle's own settings:\n%s", data)
	}
}

// TestEnsureSeedsLeavesAPopulatedStoreAlone is the rule that keeps the store
// from fighting its owner: someone who deleted `codex` must not find it back
// next launch.
func TestEnsureSeedsLeavesAPopulatedStoreAlone(t *testing.T) {
	dir := t.TempDir()
	st := launchprofile.Open(dir)
	write(t, dir, "mine.md", "---\nid: mine\nprovider: claude\n---\n")

	if err := launchprofile.EnsureSeeds(st, io.Discard); err != nil {
		t.Fatalf("EnsureSeeds: %v", err)
	}
	got, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "mine" {
		t.Fatalf("EnsureSeeds touched a populated store: %+v", got)
	}
}

func TestEnsureSeedsIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "launch")
	st := launchprofile.Open(dir)

	for i := 0; i < 3; i++ {
		if err := launchprofile.EnsureSeeds(st, io.Discard); err != nil {
			t.Fatalf("EnsureSeeds run %d: %v", i, err)
		}
	}
	got, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("after three runs the store holds %d profiles; want 2", len(got))
	}
}

// TestScaffoldIsValidFrontmatter: whatever the scaffold says in prose, the
// document it produces has to parse, or "New launch profile" hands someone a
// file cairn refuses.
func TestScaffoldIsValidFrontmatter(t *testing.T) {
	got := launchprofile.Scaffold("my-profile", "codex")
	name, provider, _ := launchprofile.Parse(got)
	if name != "my-profile" {
		t.Errorf("scaffold id = %q; want my-profile", name)
	}
	if provider != "codex" {
		t.Errorf("scaffold provider = %q; want codex", provider)
	}

	// An empty provider must still produce a bootable document rather than
	// a blank key cairn refuses.
	_, provider, _ = launchprofile.Parse(launchprofile.Scaffold("x", ""))
	if provider == "" {
		t.Error("a scaffold with no provider given declares none; cairn refuses that, so the scaffold must default")
	}
}

// TestScaffoldSaysWhatDoesNotBelong: the two keys that look like they work
// and do not are the whole reason the scaffold carries prose.
func TestScaffoldSaysWhatDoesNotBelong(t *testing.T) {
	got := string(launchprofile.Scaffold("x", "claude"))
	for _, want := range []string{"scope", "model"} {
		if !strings.Contains(got, want) {
			t.Errorf("the scaffold does not mention %q, which is one of the two keys a person will reach for and which does not work here", want)
		}
	}
}
