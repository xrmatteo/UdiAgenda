@echo off
REM ============================================================
REM  UdiAgenda - compilazione rapida (versione di prova)
REM  Serve solo Go installato: https://go.dev/dl/  (go1.24 o superiore)
REM  Non serve Visual Studio e non serve .NET.
REM  La prima compilazione scarica le dipendenze: serve internet.
REM ============================================================
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo.
  echo  ERRORE: Go non e' installato o non e' nel PATH.
  echo  Scaricalo da https://go.dev/dl/ , installalo, riapri questa finestra
  echo  e riesegui BUILD.bat
  echo.
  pause
  exit /b 1
)

set CGO_ENABLED=0
echo Scarico le dipendenze (solo la prima volta)...
go mod tidy
if errorlevel 1 (
  echo.
  echo  Download delle dipendenze FALLITO: controlla la connessione.
  pause
  exit /b 1
)

echo Compilazione in corso...
go build -ldflags="-s -w -H windowsgui" -o UdiAgenda.exe ./cmd/udiagenda
if errorlevel 1 (
  echo.
  echo  Compilazione FALLITA.
  pause
  exit /b 1
)

echo.
echo  Fatto: UdiAgenda.exe creato in %cd%
echo.
pause
