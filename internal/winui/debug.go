//go:build windows

package winui

import (
	"fmt"
	"os"
	"time"
)

var logPath string

// InitLog decide dove scrivere il file di diagnostica (di norma
// %LOCALAPPDATA%\UdiAgenda\log.txt) e lo azzera a ogni avvio.
func InitLog(defaultPath string) {
	logPath = defaultPath
	if custom := os.Getenv("UDIAGENDA_LOG"); custom != "" {
		logPath = custom
	}
	if logPath != "" {
		_ = os.WriteFile(logPath, []byte("UdiAgenda - registro diagnostico\n"), 0o644)
	}
}

// dbg scrive una riga nel file di diagnostica.
func dbg(format string, args ...any) {
	path := logPath
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}
