package apply_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/apply"
)

// scratchMakefile is agent-setup's real Makefile (~/dev/projects/agent-setup
// /Makefile), copied here as a literal string -- not read from that repo at
// test time, per this package's standing rule that no test ever runs against
// the real agent-setup checkout or the real ~/.config/agents. The canonical
// Makefile deliberately has no install-system target now: the bundle is
// self-contained, so a staged copy would only go stale. The tests below
// exercise that real absence rather than preserving the retired recipe.
//
// Staying in sync with the real file is enforced at test run time, not by a
// comment someone has to remember to update: requireMake (below) hashes the
// real ~/dev/projects/agent-setup/Makefile, if it's present on this machine,
// and fails with both hashes and a re-sync instruction the moment this
// constant drifts from it -- rather than letting every test in this package
// keep passing against a silently stale recipe.
const scratchMakefile = `.DEFAULT_GOAL := help
SHELL := /bin/bash
CAIRN ?= cairn
FIXTURE := /tmp/agent-setup-fixture

.PHONY: help list install-check install-fixture lint

help: ## Show the targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

list: ## Enumerate the catalog cairn reads out of this directory
	$(CAIRN) list --profile .

install-fixture: ## Render the installed layer into a throwaway root
	@mkdir -p $(FIXTURE)
	$(CAIRN) install base --profile . --root $(FIXTURE)
	@echo "--- rendered into $(FIXTURE) ---"
	@find $(FIXTURE) -type f | sed 's|^|  |'

install-check: ## Diff the render against the real home. Writes nothing.
	$(CAIRN) install base --profile . --check

lint: ## Shell syntax across the hooks
	@for f in hooks/*.sh; do bash -n $$f && echo "  ok  $$f"; done

# Every target above passes ` + "`" + `--profile .` + "`" + `: this directory is the catalog, and
# nothing is copied out of it. Export CAIRN_PROFILE_ROOT to this checkout to
# boot without the flag.
#
# There is no ` + "`" + `install-system` + "`" + ` any more. It staged templates/, skills/ and
# prompts/ into ~/.config/agents because the profiles named that location by
# absolute path; they name $CAIRN_PROFILE_ROOT now, so the target had nothing
# left to serve. The reasoning it was built on — that cairn must run where this
# checkout is absent — stopped holding when the catalog became the store: such
# a machine has no profiles either, and cairn refuses at the open. What it left
# behind was a second copy of this repo's content, which is what the seeder was
# and what retiring the seeder was for. ~/.config/agents is cairn's default
# bundle root and nothing else; it holds no part of this repo.
#
# ` + "`" + `cairn install` + "`" + ` with no --root writes the live ~/.claude and is human-executed,
# permanently: an agent that runs it rewrites the configuration it is running
# under, mid-session. There is deliberately no target for it.
`

func requireMake(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("make not on PATH, skipping the real Makefile check: %v", err)
	}
	requireScratchMakefileMatchesReal(t)
}

// requireScratchMakefileMatchesReal is the actual drift detector: it reads
// the real ~/dev/projects/agent-setup/Makefile -- read-only, never written
// to, per this package's standing rule -- and fails loudly if its SHA-256
// no longer matches scratchMakefile's. Without this, a change to the real
// contract (including restoring or replacing install-system) would leave
// every test in this package passing against a stale copy, silently. If the
// real file isn't present on this machine at all, it skips rather than failing
// an environment that legitimately doesn't have that checkout.
func requireScratchMakefileMatchesReal(t *testing.T) {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot locate home directory to find the real agent-setup Makefile: %v", err)
	}
	realPath := filepath.Join(home, "dev", "projects", "agent-setup", "Makefile")

	real, err := os.ReadFile(realPath)
	if os.IsNotExist(err) {
		t.Skipf("real agent-setup checkout not present at %s, skipping drift check", realPath)
		return
	}
	if err != nil {
		t.Fatalf("reading real Makefile at %s to verify scratchMakefile is not stale: %v", realPath, err)
	}

	realSum := sha256.Sum256(real)
	scratchSum := sha256.Sum256([]byte(scratchMakefile))
	if realSum != scratchSum {
		t.Fatalf(
			"scratchMakefile in invoke_real_test.go has drifted from the real Makefile at %s.\n"+
				"  real Makefile sha256:      %s\n"+
				"  scratchMakefile sha256:    %s\n"+
				"Re-copy %s's contents into the scratchMakefile constant.",
			realPath, hex.EncodeToString(realSum[:]), hex.EncodeToString(scratchSum[:]), realPath,
		)
	}
}

// TestExecRunner_RealMakefileWithoutInstallSystemSurfacesMakesRealOutput
// exercises the canonical Makefile, not a one-line stand-in. install-system
// retired when agent-setup became a self-contained bundle, so Tachyon's legacy
// Apply invocation must fail clearly and must not mutate its requested target.
// Everything runs inside scratch directories; the real checkout is only read
// by the drift guard above.
func TestExecRunner_RealMakefileWithoutInstallSystemSurfacesMakesRealOutput(t *testing.T) {
	requireMake(t)

	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	mustWrite(t, filepath.Join(bundleRoot, "Makefile"), scratchMakefile)
	sentinel := filepath.Join(agentsHome, "keep-me")
	mustWrite(t, sentinel, "unchanged\n")

	_, err := apply.Invoke(context.Background(), apply.ExecRunner(), bundleRoot, agentsHome)
	if err == nil {
		t.Fatal("Invoke succeeded against agent-setup's real Makefile; want install-system's deliberate absence surfaced")
	}
	if !strings.Contains(err.Error(), "No rule to make target") {
		t.Errorf("error = %q; want make's own missing-target message surfaced", err.Error())
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "unchanged\n" {
		t.Fatalf("Apply changed its scratch target despite the missing target: content=%q err=%v", got, readErr)
	}
}

// TestExecRunner_NoMakefileSurfacesMakesRealOutput covers the other real
// missing-target shape: no Makefile at all. Together with the canonical-file
// case above, it proves make's actual stderr survives rather than a generic
// error replacing it.
func TestExecRunner_NoMakefileSurfacesMakesRealOutput(t *testing.T) {
	requireMake(t)

	bundleRoot := t.TempDir() // no Makefile at all
	agentsHome := t.TempDir()

	_, err := apply.Invoke(context.Background(), apply.ExecRunner(), bundleRoot, agentsHome)
	if err == nil {
		t.Fatal("Invoke succeeded against a bundle with no Makefile at all; want a real failure")
	}
	if !strings.Contains(err.Error(), "No rule to make target") {
		t.Errorf("error = %q; want make's own \"No rule to make target\" message surfaced, not a generic failure", err.Error())
	}
	t.Logf("real make error surfaced: %v", err)
}
