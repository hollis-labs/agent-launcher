// Command tachyon serves the Tachyon Sysop UI.
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/hollis-labs/tachyon/internal/webui"
)

func main() {
	const addr = ":8080"

	mux := http.NewServeMux()

	// Same-origin API. The starter dashboard polls /api/health; replace
	// this with your application's real endpoints.
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// The Sysop UI — served from the embedded frontend build by go-webui.
	webui.Mount(mux)

	log.Printf("Tachyon listening on http://localhost%s/sysop/", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
