@echo off
REM ============================================================
REM  UdiAgenda - build definitiva Windows x64 (file unico, nessuna
REM  dipendenza esterna) nella cartella "dist".
REM  Serve solo Go: https://go.dev/dl/   (go1.24 o superiore)
REM ============================================================
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo.
  echo  ERRORE: Go non e' installato o non e' nel PATH.
  echo  Scaricalo da https://go.dev/dl/ e riesegui PUBLISH.bat
  echo.
  pause
  exit /b 1
)

if not exist dist mkdir dist

set CGO_ENABLED=0
echo Dipendenze...
go mod tidy
if errorlevel 1 (
  echo.
  echo  Download delle dipendenze FALLITO: controlla la connessione.
  pause
  exit /b 1
)

echo Test della logica...
go test ./internal/core/
if errorlevel 1 (
  echo.
  echo  I test sono falliti: la build si ferma qui.
  pause
  exit /b 1
)

echo Compilazione build definitiva (windows/amd64)...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w -H windowsgui" -o dist\UdiAgenda.exe ./cmd/udiagenda
if errorlevel 1 (
  echo.
  echo  Compilazione FALLITA.
  pause
  exit /b 1
)

copy /y LEGGIMI.txt dist\LEGGIMI.txt >nul

echo.
echo  Fatto: dist\UdiAgenda.exe pronto all'uso (doppio clic).
echo.
pause
