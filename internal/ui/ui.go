package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/index.html assets/styles.css assets/app.js
var files embed.FS

// FS espone i file dell'interfaccia (index.html, styles.css, app.js).
func FS() fs.FS {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

// Read legge un file dell'interfaccia.
func Read(name string) ([]byte, error) { return files.ReadFile("assets/" + name) }

// Hash è una firma del contenuto: serve per riscrivere i file su disco
// soltanto quando l'applicazione viene aggiornata.
func Hash() string {
	h := sha256.New()
	for _, n := range []string{"index.html", "styles.css", "app.js"} {
		b, _ := Read(n)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// InlineHTML restituisce l'intera interfaccia in un unico documento HTML
// (usato come soluzione di riserva se non è disponibile la mappatura
// dell'host virtuale in WebView2).
func InlineHTML() string {
	html, err := Read("index.html")
	if err != nil {
		return "<h1>Errore interfaccia</h1>"
	}
	css, _ := Read("styles.css")
	js, _ := Read("app.js")
	out := string(html)
	out = strings.Replace(out, `<link rel="stylesheet" href="styles.css"><!--INLINE-CSS-->`,
		"<style>\n"+string(css)+"\n</style>", 1)
	out = strings.Replace(out, `<script src="app.js"></script><!--INLINE-JS-->`,
		"<script>\n"+string(js)+"\n</script>", 1)
	return out
}

// Extract scrive i file dell'interfaccia in una cartella locale.
// Viene riscritta solo se il contenuto è cambiato (nuova versione).
func Extract(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stamp := filepath.Join(dir, ".version")
	if b, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(b)) == Hash() {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return nil
		}
	}
	for _, n := range []string{"index.html", "styles.css", "app.js"} {
		b, err := Read(n)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, n), b, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(stamp, []byte(Hash()), 0o644)
}
