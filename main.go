// Command tachyon is the Tachyon desktop application: an editor for the
// agent-setup bundle and a launcher for sessions over it.
//
// This binary is the whole app. There is no sidecar and no engine process:
// internal/* are packages the Wails app binds directly, which is what removes
// the spec-browsing lag the previous CLI-subprocess-per-interaction design
// measured (target architecture D2, §3). The one subprocess that remains is
// Cairn itself, invoked once per launch and never on the interactive path.
package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/hollis-labs/tachyon/internal/shell"
)

// The built frontend. It lives here rather than in internal/shell because
// go:embed cannot reach outside its own package directory, and §3 puts the
// frontend at the repo root.
//
// frontend/dist is committed: `go build ./...` has to succeed on a clean
// checkout without an npm install, and go:embed fails at compile time on a
// missing directory. Rebuild it with `npm --prefix frontend run build`.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatalf("tachyon: locating embedded frontend: %v", err)
	}

	sh, err := shell.New(shell.Config{Assets: dist})
	if err != nil {
		log.Fatalf("tachyon: %v", err)
	}
	if err := sh.Run(); err != nil {
		log.Fatalf("tachyon: %v", err)
	}
}
