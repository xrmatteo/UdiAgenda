package core

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir restituisce la cartella dove vengono salvati database e impostazioni.
// Windows: %LOCALAPPDATA%\UdiAgenda
// Altri sistemi (solo per sviluppo/test): ~/.local/share/udiagenda
func DataDir() string {
	if custom := os.Getenv("UDIAGENDA_DATA_DIR"); custom != "" {
		return custom
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "UdiAgenda")
		}
		if base := os.Getenv("APPDATA"); base != "" {
			return filepath.Join(base, "UdiAgenda")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "udiagenda")
}

// EnsureDataDir crea la cartella dati se non esiste.
func EnsureDataDir() (string, error) {
	dir := DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// DatabasePath è il percorso completo del file SQLite.
func DatabasePath() string { return filepath.Join(DataDir(), "udiagenda.db") }

// SettingsPath è il percorso completo del file impostazioni.
func SettingsPath() string { return filepath.Join(DataDir(), "settings.json") }

// --- migrazione dalla versione chiamata "Impegni" ------------------------

// legacyDataDir restituisce la vecchia cartella dati (%LOCALAPPDATA%\Impegni,
// oppure ~/.local/share/impegni fuori da Windows). Stringa vuota se ignota.
func legacyDataDir() string {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "Impegni")
		}
		if base := os.Getenv("APPDATA"); base != "" {
			return filepath.Join(base, "Impegni")
		}
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "impegni")
}

// MigrateLegacyData sposta i dati della vecchia versione "Impegni" nella
// nuova cartella "UdiAgenda", una sola volta e solo se la nuova non esiste
// ancora. Nessun dato viene perso: compiti, verifiche, voti e impostazioni
// restano quelli di prima. Se qualcosa non riesce, l'applicazione parte
// comunque con una cartella dati nuova.
func MigrateLegacyData() {
	if os.Getenv("UDIAGENDA_DATA_DIR") != "" {
		return // cartella scelta dall'utente: non si tocca niente
	}

	nuova := DataDir()
	vecchia := legacyDataDir()

	if vecchia == "" || vecchia == nuova {
		return
	}
	if _, err := os.Stat(nuova); err == nil {
		return // la nuova cartella esiste già: migrazione già fatta
	}
	if _, err := os.Stat(vecchia); err != nil {
		return // niente da migrare
	}

	if err := os.Rename(vecchia, nuova); err != nil {
		// volumi diversi o cartella occupata: si copiano i due file utili
		if err := os.MkdirAll(nuova, 0o755); err != nil {
			return
		}
		for _, nome := range []string{"impegni.db", "settings.json"} {
			_ = copyFile(filepath.Join(vecchia, nome), filepath.Join(nuova, nome))
		}
	}

	// il database cambia nome insieme all'applicazione
	vecchioDB := filepath.Join(nuova, "impegni.db")
	if _, err := os.Stat(vecchioDB); err == nil {
		if _, err := os.Stat(DatabasePath()); os.IsNotExist(err) {
			_ = os.Rename(vecchioDB, DatabasePath())
			// file di appoggio di SQLite in modalità WAL
			for _, suffisso := range []string{"-wal", "-shm"} {
				_ = os.Rename(vecchioDB+suffisso, DatabasePath()+suffisso)
			}
		}
	}
}

func copyFile(origine, destinazione string) error {
	dati, err := os.ReadFile(origine)
	if err != nil {
		return err
	}
	return os.WriteFile(destinazione, dati, 0o644)
}
