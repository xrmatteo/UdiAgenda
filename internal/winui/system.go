//go:build windows

package winui

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "UdiAgenda"
	themeKeyPath = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
)

// ExePath restituisce il percorso completo dell'eseguibile in esecuzione.
func ExePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// SetAutostart attiva o disattiva l'avvio automatico con Windows
// scrivendo nella chiave di registro dell'utente corrente
// (nessun servizio, nessun diritto di amministratore).
func SetAutostart(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if !enabled {
		err := key.DeleteValue(runValueName)
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "cannot find") {
			return nil // già assente
		}
		return nil
	}
	exe := ExePath()
	if exe == "" {
		return nil
	}
	return key.SetStringValue(runValueName, `"`+exe+`"`)
}

// AutostartEnabled indica se la voce di avvio automatico esiste e punta
// a questo eseguibile.
func AutostartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	v, _, err := key.GetStringValue(runValueName)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.Trim(v, `"`), ExePath())
}

// SystemUsesDarkTheme legge il tema corrente delle applicazioni di Windows.
func SystemUsesDarkTheme() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, themeKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	v, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false
	}
	return v == 0
}

// ShowError mostra un messaggio di errore all'utente con una finestra di Windows.
func ShowError(text string) { messageBox("UdiAgenda", text) }

// legacyRunValueName è il nome della voce di avvio automatico usata dalla
// versione precedente, quando l'applicazione si chiamava "Impegni".
const legacyRunValueName = "Impegni"

// RemoveLegacyAutostart cancella la vecchia voce di avvio automatico
// "Impegni": senza questo, dopo il cambio di nome il computer proverebbe ad
// avviare un programma che non esiste più.
func RemoveLegacyAutostart() {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(legacyRunValueName); err != nil {
		return
	}
	_ = key.DeleteValue(legacyRunValueName)
}
