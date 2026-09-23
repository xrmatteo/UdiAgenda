//go:build !windows

// Versione di sviluppo: serve la stessa interfaccia del pannello Windows
// su http://127.0.0.1:8765 per poterla collaudare su Linux/macOS.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"udiagenda/internal/core"
	"udiagenda/internal/devserver"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "indirizzo del server di sviluppo")
	flag.Parse()

	if _, err := core.EnsureDataDir(); err != nil {
		log.Fatal(err)
	}
	store, err := core.OpenStore(core.DatabasePath())
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	svc := core.NewService(store, core.SettingsPath())
	fmt.Println("UdiAgenda (anteprima interfaccia) →  http://" + *addr)
	fmt.Println("Database:", core.DatabasePath())
	log.Fatal(http.ListenAndServe(*addr, devserver.Handler(svc)))
}
