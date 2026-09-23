// Package devserver serve l'interfaccia via HTTP.
// Serve solo per provare e collaudare il pannello su un sistema non Windows:
// nella versione per Windows l'interfaccia viene caricata dentro WebView2.
package devserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"udiagenda/internal/core"
	"udiagenda/internal/ui"
)

// Handler costruisce il router HTTP di sviluppo.
func Handler(svc *core.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(ui.FS())))
	mux.HandleFunc("/rpc/", func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/rpc/")
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var raw json.RawMessage
		if len(body) > 0 && string(body) != "null" {
			raw = json.RawMessage(body)
		}
		res, err := svc.Call(method, raw)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": res})
	})
	return mux
}
