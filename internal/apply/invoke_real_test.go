package apply_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/tachyon/internal/apply"
)

// scratchMakefile is agent-setup's real Makefile (~/dev/projects/agent-setup
// /Makefile), copied here as a literal string -- not read from that repo at
// test time, per this task's own standing rule that no test ever points at
// the real agent-setup checkout or the real ~/.config/agents. It was
// verified byte-for-byte identical against that file on 2026-09-04 (sha256
// 610e1dce7fc8af9855e12006812c541e5992b70c625151c0edd7cbd5a22cddfe as of
// that date) before this task began any work, and this string is what the
// tests below actually exercise install-system's real recipe against --
// three real rsyncs, not a stand-in.
const scratchMakefile = `.DEFAULT_GOAL := help
SHELL := /bin/bash
CAIRN ?= cairn
FIXTURE := /tmp/agent-setup-fixture
AGENTS_HOME ?= $(HOME)/.config/agents

.PHONY: help install-system list install-check install-fixture lint

help: ## Show the targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

install-system: ## Stage templates/, skills/ and prompts/ into ~/.config/agents (the installed location)
	@mkdir -p $(AGENTS_HOME)
	rsync -a --delete --delete-excluded --exclude='.DS_Store' templates/ $(AGENTS_HOME)/templates/
	rsync -a --delete --delete-excluded --exclude='.DS_Store' skills/    $(AGENTS_HOME)/skills/
	rsync -a --delete --delete-excluded --exclude='.DS_Store' prompts/   $(AGENTS_HOME)/prompts/
	@echo "staged into $(AGENTS_HOME)"

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
`

func requireMake(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("make not on PATH, skipping the real make install-system check: %v", err)
	}
}

// TestExecRunner_RealMakeStagesAndActuallyDeletes is the acceptance bullet
// "the real make install-system actually runs and actually deletes a
// staged file absent from the bundle" -- proven against a real make
// binary and real rsync, entirely inside two scratch directories this test
// owns. Never touches ~/.config/agents or ~/dev/projects/agent-setup.
func TestExecRunner_RealMakeStagesAndActuallyDeletes(t *testing.T) {
	requireMake(t)

	bundleRoot := t.TempDir()
	agentsHome := t.TempDir()

	mustWrite(t, filepath.Join(bundleRoot, "Makefile"), scratchMakefile)
	mustWrite(t, filepath.Join(bundleRoot, "templates", "agents.md"), "template body\n")
	mustWrite(t, filepath.Join(bundleRoot, "skills", "demo", "SKILL.md"), "skill body\n")
	mustWrite(t, filepath.Join(bundleRoot, "prompts", "hello.md"), "prompt body\n")

	// A file staged at the destination that the bundle does not have --
	// what --delete must remove.
	orphan := filepath.Join(agentsHome, "prompts", "orphan.md")
	mustWrite(t, orphan, "leftover from a previous stage\n")
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("setup: orphan file not present before Apply: %v", err)
	}

	// Sanity: Compare must read this as "differs" before Apply runs.
	before, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (before): %v", err)
	}
	if !before.Differs {
		t.Fatalf("Compare (before) = %+v; want Differs=true", before)
	}

	result, err := apply.Invoke(context.Background(), apply.ExecRunner(), bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Invoke (real make install-system): %v", err)
	}
	t.Logf("make install-system stdout:\n%s", result.Stdout)
	if !strings.Contains(result.Stdout, "staged into "+agentsHome) {
		t.Errorf("Stdout = %q; want it to contain the Makefile's own \"staged into %s\" line", result.Stdout, agentsHome)
	}

	// The orphan file, staged but absent from the bundle, must actually be
	// gone -- this is --delete really happening, not merely asserted.
	if _, statErr := os.Stat(orphan); !os.IsNotExist(statErr) {
		t.Fatalf("orphan file %s still exists after Apply; --delete did not run", orphan)
	}

	// The bundle's own content must actually be staged.
	for _, rel := range []string{
		filepath.Join("templates", "agents.md"),
		filepath.Join("skills", "demo", "SKILL.md"),
		filepath.Join("prompts", "hello.md"),
	} {
		got, readErr := os.ReadFile(filepath.Join(agentsHome, rel))
		if readErr != nil {
			t.Fatalf("reading staged %s: %v", rel, readErr)
		}
		want, readErr := os.ReadFile(filepath.Join(bundleRoot, rel))
		if readErr != nil {
			t.Fatalf("reading bundle %s: %v", rel, readErr)
		}
		if string(got) != string(want) {
			t.Errorf("staged %s = %q; want %q", rel, got, want)
		}
	}

	// After Apply, enablement must go dark again -- bundle and staged layer
	// now match.
	after, err := apply.Compare(bundleRoot, agentsHome)
	if err != nil {
		t.Fatalf("Compare (after): %v", err)
	}
	if after.Differs {
		t.Fatalf("Compare (after) = %+v; want Differs=false immediately after a real Apply", after)
	}
}

// TestExecRunner_NoMakefileSurfacesMakesRealOutput and
// TestExecRunner_MakefileWithoutTargetSurfacesMakesRealOutput are the
// acceptance bullet "make/target-missing failure surfaces make's own
// output, not a generic error" -- both simulated by pointing at a real
// scratch directory that make itself refuses, and reading the *actual*
// stderr make produced, not a canned message.
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

func TestExecRunner_MakefileWithoutTargetSurfacesMakesRealOutput(t *testing.T) {
	requireMake(t)

	bundleRoot := t.TempDir()
	mustWrite(t, filepath.Join(bundleRoot, "Makefile"), "help:\n\t@echo hi\n")
	agentsHome := t.TempDir()

	_, err := apply.Invoke(context.Background(), apply.ExecRunner(), bundleRoot, agentsHome)
	if err == nil {
		t.Fatal("Invoke succeeded against a Makefile with no install-system target; want a real failure")
	}
	if !strings.Contains(err.Error(), "No rule to make target") {
		t.Errorf("error = %q; want make's own \"No rule to make target\" message surfaced, not a generic failure", err.Error())
	}
	t.Logf("real make error surfaced: %v", err)
}
