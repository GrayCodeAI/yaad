package server

import (
	_ "embed"
	"net/http"
)

//go:embed dashboard.html
var dashboardHTML []byte

// ServeDashboard serves the graph visualization dashboard at /yaad/ui.
func ServeDashboard(mux *http.ServeMux) {
	mux.HandleFunc("GET /yaad/ui", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(dashboardHTML)
	})
}
