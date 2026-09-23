module udiagenda

go 1.24.0

// Le dipendenze (WebView2, SQLite in pura Go, golang.org/x/sys) vengono
// risolte e bloccate da "go mod tidy" alla prima compilazione: lo fanno
// da soli BUILD.bat e PUBLISH.bat.
