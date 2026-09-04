// Package apply is the manager's Apply action: comparing the active
// bundle's staged three directories (templates/, skills/, prompts/)
// against the installed layer, and running the one command that mirrors
// one onto the other — `make install-system`, in the bundle root.
//
// # Why this exists (CW-20260904-0023, T29)
//
// Four of the manager's seven editable artifact kinds — templates, role
// prose, skills and prompts — resolve from the *installed* layer
// (~/.config/agents by default) when Cairn boots, not from the bundle a
// person edits in Tachyon. Editing one of those in the manager changes
// nothing about any boot until it is staged there. `make install-system`
// (agent-setup's own Makefile) is what stages it: three `rsync -a
// --delete --delete-excluded --exclude='.DS_Store'` calls, one per
// directory.
//
// This package runs exactly that command — never a hand-rolled
// reimplementation of its rsyncs — and answers, ahead of running it,
// whether the bundle and the installed layer actually differ, and what
// differs. Both questions are content-based tree comparisons ([Compare]),
// not a dirty flag: editing a file and reverting it must read as "nothing
// to stage" again, exactly as if the edit had never happened.
//
// # Staging is deliberate, not implicit (D3 as corrected by Chrispian,
// 2026-09-04)
//
// Nothing in this package runs on its own. [Compare] only reads; it never
// writes anywhere. [Invoke] is the one function that actually runs `make
// install-system`, and it is called from exactly one place in the whole
// app: the frontend's explicit, user-clicked Apply button, after a
// confirmation that names the destination and says plainly that staged
// files absent from the bundle are deleted. Not on Save, not on launch,
// not on a bundle-root change — see this package's own tests, and
// Manager.jsx's ApplyBar, for where that discipline is enforced.
//
// `cairn install` is never invoked by anything in this package, or
// anywhere else this task touches — that is a separate, larger-blast-radius
// capability (it writes the live ~/.claude) left for a future task.
//
// # No backups (deferred, deliberately)
//
// Chrispian's own call: backups or snapshots before Apply are explicitly
// out of scope here, deferred until the rest of this design is stable. This
// package does not build them, and does not stub a TODO implying they are
// coming — see CW-20260904-0023's own "explicitly deferred" section.
package apply
