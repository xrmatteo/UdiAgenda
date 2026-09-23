//go:build windows

// UdiAgenda — pannello laterale a scomparsa per compiti, verifiche,
// appuntamenti e scadenze. Applicazione singola, dati locali in SQLite.
package main

import (
	"os"

	"udiagenda/internal/core"
	"udiagenda/internal/winui"
)

func main() {
	// una sola copia in esecuzione: la seconda apre il pannello della prima
	if winui.AlreadyRunning() {
		return
	}

	// dalla versione "Impegni": porta con sé dati e impostazioni già presenti
	core.MigrateLegacyData()
	winui.RemoveLegacyAutostart()

	if _, err := core.EnsureDataDir(); err != nil {
		winui.ShowError("Impossibile creare la cartella dati:\n" + err.Error())
		return
	}
	// primo avvio in assoluto (nessun file impostazioni): apro il pannello
	firstRun := false
	if _, err := os.Stat(core.SettingsPath()); os.IsNotExist(err) {
		firstRun = true
		// impostazioni iniziali (avvio automatico attivo, tema automatico)
		_ = core.SaveSettings(core.SettingsPath(), core.DefaultSettings())
	}

	store, err := core.OpenStore(core.DatabasePath())
	if err != nil {
		winui.ShowError("Impossibile aprire il database:\n" + err.Error())
		return
	}
	defer store.Close()

	svc := core.NewService(store, core.SettingsPath())
	if err := winui.Run(svc, firstRun); err != nil {
		winui.ShowError(err.Error())
	}
}
