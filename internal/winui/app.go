//go:build windows

package winui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"

	"udiagenda/internal/core"
	"udiagenda/internal/ui"
)

const (
	classTab   = "UdiAgendaTabWindow"
	classPanel = "UdiAgendaPanelWindow"
	mutexName  = `Local\UdiAgendaSingleInstance`
	animMillis = 190
	timerAnim  = 1
	timerWarm  = 2
	timerWarn  = 3
	timerPeek  = 4
	warmMillis = 2500
	warnMillis = 4000
	// linguetta a scomparsa: frequenza del controllo, velocità della
	// dissolvenza, inattività dopo la quale sparisce e periodi di cortesia
	// in cui resta comunque visibile.
	peekMillis       = 50
	slideSteps       = 4
	idleHideMillis   = 6000
	startGraceMillis = 4000
	closeGraceMillis = 1200
	// zona di prossimità attorno alla linguetta (in punti, poi scalata)
	peekReachDip  = 60.0
	peekBandDip   = 60.0
	tabWidthDip   = 16.0
	tabHeightDip  = 112.0
	panelEdgeDip  = 8.0
	virtualHost   = "udiagenda.local"
	panelStartURL = "https://" + virtualHost + "/index.html"
)

type geometry struct {
	scale                          float64
	work                           rect
	panelW, panelH, panelX, panelY int32
	hiddenX                        int32
	tabW, tabH, tabY               int32
	right                          bool
}

// App tiene insieme finestre, WebView2 e servizio dati.
type App struct {
	svc      *core.Service
	inst     windows.Handle
	tab      windows.Handle
	panel    windows.Handle
	chromium *edge.Chromium
	font     windows.Handle

	set core.Settings
	geo geometry

	open      bool
	closing   bool
	modal     bool
	hover     bool
	animating bool
	curX      int32
	animFrom  int32
	animTo    int32
	animStart time.Time
	openedAt  time.Time
	closedAt  time.Time
	// jsAlive diventa vero appena la pagina dell'interfaccia risponde
	jsAlive  bool
	showWarn bool
	// dismissed indica che il pannello si è chiuso perché l'utente ha
	// cliccato fuori: se quel clic era sulla linguetta non va riaperto.
	dismissed bool
	quitting  bool

	// --- linguetta a scomparsa: si ritrae dentro il bordo dello schermo
	tabOff       int32 // quanto è rientrata (0 = tutta visibile, tabW = nascosta)
	tabOffTarget int32
	tabShown     bool
	lastCursor   point
	lastMove     time.Time
	graceUntil   time.Time
}

var app *App

// AlreadyRunning verifica che non ci sia già un'altra copia in esecuzione.
// In tal caso apre il pannello della copia esistente e restituisce true.
func AlreadyRunning() bool {
	h, _, err := pCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(utf16(mutexName))))
	if h == 0 {
		return false
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		if hwnd, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(utf16(classTab))), 0); hwnd != 0 {
			pPostMessageW.Call(hwnd, messageOpenPanel, 0, 0)
		}
		return true
	}
	return false
}

// Run avvia l'interfaccia: crea le finestre, carica il pannello e
// resta in ascolto dei messaggi di Windows fino alla chiusura.
func Run(svc *core.Service, openAtStart bool) error {
	InitLog(filepath.Join(core.DataDir(), "log.txt"))
	dbg("avvio, dati in %s", core.DataDir())
	enableDPIAwareness()

	a := &App{svc: svc, set: svc.Settings()}
	app = a

	svc.OnClosePanel = func() { a.Close() }
	svc.OnQuit = func() { a.Quit() }
	svc.OnModal = func(open bool) { a.modal = open }
	svc.SystemDark = SystemUsesDarkTheme
	svc.OnSettingsChanged = func(s core.Settings) { a.applySettings(s) }

	// L'avvio automatico rispecchia l'impostazione salvata (e aggiorna il
	// percorso se il programma è stato spostato).
	_ = SetAutostart(a.set.StartWithWindows)

	if err := a.createWindows(); err != nil {
		return err
	}
	if err := a.createWebView(); err != nil {
		return err
	}
	a.applyGeometry()
	a.tabOff, a.tabOffTarget, a.tabShown = 0, 0, true
	showWindow(a.tab, swShowNoActive)
	a.lastMove = time.Now()
	a.graceUntil = time.Now().Add(startGraceMillis * time.Millisecond)
	pSetTimer.Call(uintptr(a.tab), timerPeek, peekMillis, 0)
	// controllo di sicurezza: se entro qualche secondo la pagina non da'
	// segni di vita, si ricarica in modo alternativo e poi si avvisa
	pSetTimer.Call(uintptr(a.panel), timerWarm, warmMillis, 0)
	if openAtStart {
		// primo avvio in assoluto: mostro subito il pannello una volta
		a.Open()
	}
	a.messageLoop()
	return nil
}

// ------------------------------------------------------------------ finestre

func (a *App) createWindows() error {
	a.inst = moduleHandle()
	cursor, _, _ := pLoadCursorW.Call(0, idcHand)
	arrow, _, _ := pLoadCursorW.Call(0, idcArrow)

	tabClass := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   windows.NewCallback(tabProc),
		HInstance:     a.inst,
		HCursor:       windows.Handle(cursor),
		LpszClassName: utf16(classTab),
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&tabClass))); r == 0 {
		return fmt.Errorf("registrazione finestra linguetta: %v", err)
	}
	panelClass := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   windows.NewCallback(panelProc),
		HInstance:     a.inst,
		HCursor:       windows.Handle(arrow),
		LpszClassName: utf16(classPanel),
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&panelClass))); r == 0 {
		return fmt.Errorf("registrazione finestra pannello: %v", err)
	}

	tab, _, err := pCreateWindowExW.Call(
		wsExTopmost|wsExToolWin|wsExNoActive,
		uintptr(unsafe.Pointer(utf16(classTab))),
		uintptr(unsafe.Pointer(utf16("UdiAgenda"))),
		wsPopup, 0, 0, 10, 10, 0, 0, uintptr(a.inst), 0)
	if tab == 0 {
		return fmt.Errorf("creazione linguetta: %v", err)
	}
	a.tab = windows.Handle(tab)

	panel, _, err := pCreateWindowExW.Call(
		wsExTopmost|wsExToolWin,
		uintptr(unsafe.Pointer(utf16(classPanel))),
		uintptr(unsafe.Pointer(utf16("UdiAgenda"))),
		wsPopup, 0, 0, 400, 600, 0, 0, uintptr(a.inst), 0)
	if panel == 0 {
		return fmt.Errorf("creazione pannello: %v", err)
	}
	a.panel = windows.Handle(panel)

	roundCorners(a.tab, dwmCornerSmall)
	roundCorners(a.panel, dwmCornerRound)
	return nil
}

func (a *App) createWebView() error {
	// modalità di collaudo: finestre Win32 senza WebView2 (usata per i test)
	if os.Getenv("UDIAGENDA_NO_WEBVIEW") == "1" {
		return nil
	}
	dataDir := filepath.Join(core.DataDir(), "webview")
	uiDir := filepath.Join(core.DataDir(), "ui")
	_ = os.MkdirAll(dataDir, 0o755)
	extractErr := ui.Extract(uiDir)
	if extractErr != nil {
		dbg("estrazione interfaccia FALLITA: %v", extractErr)
	} else if st, err := os.Stat(filepath.Join(uiDir, "index.html")); err == nil {
		dbg("interfaccia estratta in %s (index.html %d byte)", uiDir, st.Size())
	} else {
		dbg("index.html non trovato dopo l'estrazione: %v", err)
	}

	// La finestra del pannello viene mostrata (fuori dallo schermo) prima di
	// creare WebView2: alcune versioni non agganciano la superficie di
	// disegno se la finestra ospite non e' mai stata visibile.
	a.geo = a.computeGeometry()
	a.curX = a.geo.hiddenX
	setWindowPos(a.panel, hwndTopmost, a.curX, a.geo.panelY, a.geo.panelW, a.geo.panelH,
		uint32(swpNoActivate|swpShowWindow))
	dbg("pannello %dx%d a x=%d y=%d (scala %.2f, area di lavoro %d,%d-%d,%d)",
		a.geo.panelW, a.geo.panelH, a.curX, a.geo.panelY, a.geo.scale,
		a.geo.work.Left, a.geo.work.Top, a.geo.work.Right, a.geo.work.Bottom)

	// lingua italiana e sfondo coerente con il pannello
	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--lang=it-IT")

	c := edge.NewChromium()
	c.DataPath = dataDir
	c.MessageCallback = a.onWebMessage
	a.chromium = c

	c.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		dbg("navigazione completata")
	}
	if !c.Embed(uintptr(a.panel)) {
		dbg("WebView2 non disponibile: Embed fallito")
		messageBox("UdiAgenda",
			"Non è stato possibile avviare il componente WebView2 di Windows.\n\n"+
				"Windows 11 lo include di serie: se il problema persiste, installa\n"+
				"\"Microsoft Edge WebView2 Runtime\" e riavvia il programma.")
		return fmt.Errorf("webview2 non disponibile")
	}
	dbg("WebView2 avviato")
	_ = c.Show()
	if ctrl := c.GetController(); ctrl != nil {
		// sfondo opaco: se la pagina non si carica si vede comunque un
		// pannello pieno e non una finestra trasparente
		if c2 := ctrl.GetICoreWebView2Controller2(); c2 != nil {
			bg := edge.COREWEBVIEW2_COLOR{A: 255, R: 0xF7, G: 0xF7, B: 0xFA}
			if a.darkTheme() {
				bg = edge.COREWEBVIEW2_COLOR{A: 255, R: 0x20, G: 0x20, B: 0x24}
			}
			if err := c2.PutDefaultBackgroundColor(bg); err != nil {
				dbg("colore di sfondo non impostato: %v", err)
			}
		}
	}
	c.Resize()
	if s, err := c.GetSettings(); err == nil {
		_ = s.PutAreDefaultContextMenusEnabled(false)
		_ = s.PutAreDevToolsEnabled(false)
		_ = s.PutIsStatusBarEnabled(false)
		_ = s.PutIsZoomControlEnabled(false)
		_ = s.PutIsSwipeNavigationEnabled(false)
	}

	loaded := false
	if extractErr == nil {
		if w3 := c.GetICoreWebView2_3(); w3 != nil {
			err := w3.SetVirtualHostNameToFolderMapping(virtualHost, uiDir,
				edge.COREWEBVIEW2_HOST_RESOURCE_ACCESS_KIND_ALLOW)
			dbg("host virtuale %s -> %s (esito: %v)", virtualHost, uiDir, err)
			c.Navigate(panelStartURL)
			loaded = true
		} else {
			dbg("interfaccia ICoreWebView2_3 non disponibile")
		}
	}
	if !loaded {
		dbg("carico l'interfaccia come documento unico")
		c.NavigateToString(ui.InlineHTML())
	}
	return nil
}

// darkTheme dice se disegnare in scuro (impostazione dell'utente o di Windows).
func (a *App) darkTheme() bool {
	switch a.set.Theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return SystemUsesDarkTheme()
}

// ------------------------------------------------------------------ geometria

func (a *App) computeGeometry() geometry {
	g := geometry{scale: dpiScale(a.panel), work: workArea()}
	if g.scale <= 0 {
		g.scale = 1
	}
	px := func(dip float64) int32 { return int32(dip*g.scale + 0.5) }

	g.right = a.set.TabSide != "left"
	gap := px(panelEdgeDip)
	g.panelW = px(float64(a.set.PanelWidth))
	if max := g.work.width() - 2*gap; g.panelW > max && max > 0 {
		g.panelW = max
	}
	g.panelH = g.work.height() - 2*gap
	g.panelY = g.work.Top + gap

	if g.right {
		g.panelX = g.work.Right - g.panelW - gap
		g.hiddenX = g.work.Right
	} else {
		g.panelX = g.work.Left + gap
		g.hiddenX = g.work.Left - g.panelW
	}

	g.tabW = px(tabWidthDip)
	g.tabH = px(tabHeightDip)
	switch a.set.TabPosition {
	case "top":
		g.tabY = g.work.Top + px(28)
	case "bottom":
		g.tabY = g.work.Bottom - g.tabH - px(28)
	default:
		g.tabY = g.work.Top + (g.work.height()-g.tabH)/2
	}
	g.tabY = clampInt32(g.tabY, g.work.Top, g.work.Bottom-g.tabH)
	return g
}

// applyGeometry riposiziona linguetta e pannello (all'avvio, al cambio di
// impostazioni, di risoluzione, di scaling o di barra delle applicazioni).
func (a *App) applyGeometry() {
	a.geo = a.computeGeometry()
	if !a.open {
		a.curX = a.geo.hiddenX
	} else {
		a.curX = a.geo.panelX
	}
	setWindowPos(a.panel, hwndTopmost, a.curX, a.geo.panelY, a.geo.panelW, a.geo.panelH, uint32(swpNoActivate))
	a.resizeWebView()
	a.placeTab()
	invalidate(a.tab)
}

func (a *App) placeTab() {
	r := a.tabRect()
	setWindowPos(a.tab, hwndTopmost, r.Left, r.Top, a.geo.tabW, a.geo.tabH, uint32(swpNoActivate))
}

func (a *App) resizeWebView() {
	if a.chromium == nil {
		return
	}
	a.chromium.Resize()
}

// ------------------------------------------------------------ apri / chiudi

// Toggle apre il pannello se è chiuso e lo chiude se è aperto.
func (a *App) Toggle() {
	if a.open {
		a.dismissed = false
		a.Close()
		return
	}
	// Se il pannello si è appena chiuso perché questo stesso clic gli ha
	// tolto il fuoco, il clic sulla linguetta non deve riaprirlo subito.
	if a.dismissed && time.Since(a.closedAt) < 400*time.Millisecond {
		a.dismissed = false
		return
	}
	a.Open()
}

// Open mostra il pannello con l'animazione di scorrimento.
func (a *App) Open() {
	if a.open && !a.closing {
		return
	}
	a.geo = a.computeGeometry()
	if !a.open {
		if !isVisible(a.panel) {
			a.curX = a.geo.hiddenX
		}
		setWindowPos(a.panel, hwndTopmost, a.curX, a.geo.panelY, a.geo.panelW, a.geo.panelH,
			uint32(swpNoActivate|swpShowWindow))
		a.resizeWebView()
		if a.chromium != nil {
			_ = a.chromium.Show()
			_ = a.chromium.NotifyParentWindowPositionChanged()
		}
	}
	a.open = true
	a.closing = false
	a.modal = false
	a.dismissed = false
	if a.chromium != nil {
		a.chromium.Eval("window.__udiagendaRefresh && window.__udiagendaRefresh()")
	}
	pSetForegroundWindow.Call(uintptr(a.panel))
	if a.chromium != nil {
		a.chromium.Focus()
	}
	a.openedAt = time.Now()
	a.showTabNow()
	a.startAnim(a.geo.panelX)
	invalidate(a.tab)
}

// Close richiude il pannello lasciando visibile solo la linguetta.
func (a *App) Close() {
	if !a.open && !isVisible(a.panel) {
		return
	}
	a.open = false
	a.closing = true
	a.modal = false
	a.closedAt = time.Now()
	a.graceUntil = time.Now().Add(closeGraceMillis * time.Millisecond)
	a.startAnim(a.geo.hiddenX)
	invalidate(a.tab)
}

// Quit chiude completamente l'applicazione.
func (a *App) Quit() {
	a.quitting = true
	pDestroyWindow.Call(uintptr(a.panel))
	pDestroyWindow.Call(uintptr(a.tab))
	pPostQuitMessage.Call(0)
}

func (a *App) startAnim(to int32) {
	dbg("startAnim da %d a %d (animating=%v)", a.curX, to, a.animating)
	a.animFrom = a.curX
	a.animTo = to
	a.animStart = time.Now()
	if a.animFrom == a.animTo {
		a.finishAnim()
		return
	}
	if !a.animating {
		a.animating = true
		pSetTimer.Call(uintptr(a.panel), timerAnim, 10, 0)
	}
}

func (a *App) stepAnim() {
	p := float64(time.Since(a.animStart).Milliseconds()) / float64(animMillis)
	if p >= 1 {
		a.curX = a.animTo
		a.finishAnim()
		return
	}
	// easing "ease-out cubic": parte veloce e rallenta alla fine
	e := 1 - (1-p)*(1-p)*(1-p)
	a.curX = a.animFrom + int32(float64(a.animTo-a.animFrom)*e)
	setWindowPos(a.panel, 0, a.curX, a.geo.panelY, a.geo.panelW, a.geo.panelH,
		uint32(swpNoActivate|swpNoZOrder|swpNoSize))
	a.placeTab()
}

func (a *App) finishAnim() {
	dbg("finishAnim x=%d closing=%v", a.curX, a.closing)
	a.animating = false
	pKillTimer.Call(uintptr(a.panel), timerAnim)
	setWindowPos(a.panel, 0, a.curX, a.geo.panelY, a.geo.panelW, a.geo.panelH,
		uint32(swpNoActivate|swpNoZOrder|swpNoSize))
	a.placeTab()
	if a.closing {
		a.closing = false
		showWindow(a.panel, swHide)
	}
	if a.chromium != nil {
		_ = a.chromium.NotifyParentWindowPositionChanged()
	}
}

// applySettings viene richiamata quando l'utente salva le impostazioni.
func (a *App) applySettings(s core.Settings) {
	prev := a.set
	a.set = s
	if prev.StartWithWindows != s.StartWithWindows {
		_ = SetAutostart(s.StartWithWindows)
	}
	a.applyGeometry()
	if !s.HideTab {
		a.showTabNow()
	} else {
		a.graceUntil = time.Now().Add(closeGraceMillis * time.Millisecond)
	}
	if a.open {
		a.curX = a.geo.panelX
		setWindowPos(a.panel, hwndTopmost, a.curX, a.geo.panelY, a.geo.panelW, a.geo.panelH,
			uint32(swpNoActivate|swpShowWindow))
		a.resizeWebView()
		a.placeTab()
	}
}

// ------------------------------------------------------------------ ponte JS

type rpcRequest struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (a *App) onWebMessage(raw string) {
	var req rpcRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return
	}
	if !a.jsAlive {
		a.jsAlive = true
		dbg("interfaccia caricata correttamente (prima richiesta: %s)", req.Method)
	}
	res, err := a.svc.Call(req.Method, req.Params)
	if err != nil {
		b, _ := json.Marshal(err.Error())
		a.reply(req.ID, false, b)
		return
	}
	b, mErr := json.Marshal(res)
	if mErr != nil {
		b, _ = json.Marshal("errore interno: " + mErr.Error())
		a.reply(req.ID, false, b)
		return
	}
	a.reply(req.ID, true, b)
}

func (a *App) reply(id int, ok bool, payload []byte) {
	if a.chromium == nil {
		return
	}
	a.chromium.Eval(fmt.Sprintf("window.__udiagendaReply(%d,%t,%s)", id, ok, string(payload)))
}

// ------------------------------------------------------------ ciclo messaggi

func (a *App) messageLoop() {
	var m msgStruct
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func def(hwnd, msg, wp, lp uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// ------------------------------------------------------------ finestra linguetta

func tabProc(hwnd, msg, wp, lp uintptr) uintptr {
	a := app
	if a == nil {
		return def(hwnd, msg, wp, lp)
	}
	switch msg {
	case wmPaint:
		a.paintTab(windows.Handle(hwnd))
		return 0
	case 0x0014: // WM_ERASEBKGND
		return 1
	case wmLButtonUp:
		dbg("linguetta: clic (open=%v closing=%v animating=%v)", a.open, a.closing, a.animating)
		a.Toggle()
		return 0
	case wmRButtonUp:
		a.showTabMenu()
		return 0
	case wmTimer:
		if wp == timerPeek {
			a.peekTick()
		}
		return 0
	case wmMouseMove:
		if !a.hover {
			a.hover = true
			t := trackMouseEvent{CbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), DwFlags: tmeLeave, HwndTrack: windows.Handle(hwnd)}
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
			invalidate(windows.Handle(hwnd))
		}
		return 0
	case wmMouseLeave:
		a.hover = false
		invalidate(windows.Handle(hwnd))
		return 0
	case messageOpenPanel:
		a.Open()
		return 0
	case wmSettingChange, wmDisplayChange, wmDpiChanged:
		if !a.animating {
			a.applyGeometry()
		}
		return 0
	case wmDestroy:
		if !a.quitting {
			pPostQuitMessage.Call(0)
		}
		return 0
	}
	return def(hwnd, msg, wp, lp)
}

// ------------------------------------------------ linguetta a scomparsa

// showTabNow rende la linguetta subito visibile (pannello aperto, avvio,
// oppure opzione disattivata).
func (a *App) showTabNow() {
	a.tabOffTarget = 0
	a.tabOff = 0
	if !a.tabShown {
		a.tabShown = true
		showWindow(a.tab, swShowNoActive)
	}
	a.placeTab()
}

// peekZone è l'area in cui basta avvicinare il puntatore perché la
// linguetta ricompaia: una fascia attorno alla linguetta stessa.
func (a *App) peekZone() rect {
	g := a.geo
	reach := int32(peekReachDip*g.scale + 0.5)
	band := int32(peekBandDip*g.scale + 0.5)
	var x0, x1 int32
	if g.right {
		x0, x1 = g.work.Right-g.tabW-reach, g.work.Right
	} else {
		x0, x1 = g.work.Left, g.work.Left+g.tabW+reach
	}
	return rect{Left: x0, Top: g.tabY - band, Right: x1, Bottom: g.tabY + g.tabH + band}
}

func inRect(r rect, p point) bool {
	return p.X >= r.Left && p.X <= r.Right && p.Y >= r.Top && p.Y <= r.Bottom
}

// peekTick decide se la linguetta deve essere visibile.
// Viene richiamata dal timer della linguetta.
func (a *App) peekTick() {
	if !a.set.HideTab || a.open || a.animating || a.closing {
		if a.tabOff != 0 || !a.tabShown {
			a.showTabNow()
		}
		return
	}

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if pt != a.lastCursor {
		a.lastCursor = pt
		a.lastMove = time.Now()
	}

	show := inRect(a.peekZone(), pt)
	if show && time.Since(a.lastMove) > idleHideMillis*time.Millisecond {
		// puntatore fermo da un po': la linguetta si toglie di mezzo,
		// a meno che non sia proprio sotto al puntatore
		if !inRect(a.tabRect(), pt) {
			show = false
		}
	}
	if time.Now().Before(a.graceUntil) {
		show = true
	}

	want := a.geo.tabW
	if show {
		want = 0
	}
	if want != a.tabOffTarget {
		z := a.peekZone()
		dbg("linguetta -> %s (cursore %d,%d, zona %d,%d-%d,%d, ferma da %dms)",
			map[bool]string{true: "visibile", false: "nascosta"}[show],
			pt.X, pt.Y, z.Left, z.Top, z.Right, z.Bottom, time.Since(a.lastMove).Milliseconds())
	}
	a.tabOffTarget = want
	a.stepPeek()
}

// tabRect è la posizione attuale della linguetta sullo schermo.
func (a *App) tabRect() rect {
	g := a.geo
	var x int32
	if g.right {
		x = a.curX - g.tabW + a.tabOff
	} else {
		x = a.curX + g.panelW - a.tabOff
	}
	x = clampInt32(x, g.work.Left-g.tabW, g.work.Right)
	return rect{Left: x, Top: g.tabY, Right: x + g.tabW, Bottom: g.tabY + g.tabH}
}

// stepPeek fa scorrere la linguetta dentro o fuori dal bordo dello schermo.
func (a *App) stepPeek() {
	if a.tabOff == a.tabOffTarget {
		return
	}
	step := a.geo.tabW / slideSteps
	if step < 2 {
		step = 2
	}
	if a.tabOff < a.tabOffTarget {
		a.tabOff += step
		if a.tabOff > a.tabOffTarget {
			a.tabOff = a.tabOffTarget
		}
	} else {
		a.tabOff -= step
		if a.tabOff < a.tabOffTarget {
			a.tabOff = a.tabOffTarget
		}
		if !a.tabShown {
			a.tabShown = true
			showWindow(a.tab, swShowNoActive)
		}
	}
	a.placeTab()
	if a.tabOff >= a.geo.tabW && a.tabShown {
		a.tabShown = false
		showWindow(a.tab, swHide)
	}
}

// tabColors restituisce il colore della linguetta e quello della freccia,
// in base al colore d'accento scelto nelle impostazioni e al tema in uso.
func (a *App) tabColors() (bg, ink uintptr) {
	acc := core.AccentByKey(a.set.Accent)
	dark := a.darkTheme()
	c := acc.Light
	if dark {
		c = acc.Dark
	}
	r, g, b := uint32(c[0]), uint32(c[1]), uint32(c[2])
	if a.hover {
		if dark {
			r, g, b = mix(r, 0, .14), mix(g, 0, .14), mix(b, 0, .14)
		} else {
			r, g, b = mix(r, 255, .16), mix(g, 255, .16), mix(b, 255, .16)
		}
	}
	ink = rgb(255, 255, 255)
	if dark {
		ink = rgb(0x14, 0x16, 0x1A)
	}
	return rgb(r, g, b), ink
}

func mix(from, to uint32, t float64) uint32 {
	return uint32(float64(from) + (float64(to)-float64(from))*t)
}

// showTabMenu mostra il menu con il tasto destro sulla linguetta:
// resta raggiungibile anche se il pannello non dovesse funzionare.
func (a *App) showTabMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	open := "Apri il pannello"
	if a.open {
		open = "Chiudi il pannello"
	}
	pAppendMenuW.Call(menu, mfString, 1, uintptr(unsafe.Pointer(utf16(open))))
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	pAppendMenuW.Call(menu, mfString, 2, uintptr(unsafe.Pointer(utf16("Esci da UdiAgenda"))))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	align := uintptr(tpmLeftAlign)
	if a.geo.right {
		align = tpmRightAlign
	}
	// il menu va in primo piano sulla finestra del pannello, altrimenti
	// non si chiude quando si clicca altrove
	pSetForegroundWindow.Call(uintptr(a.panel))
	cmd, _, _ := pTrackPopupMenu.Call(menu, align|tpmRightBtn|tpmReturnCmd|tpmNoAnimate,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(a.panel), 0)
	pPostMessageW.Call(uintptr(a.panel), 0, 0, 0)
	switch cmd {
	case 1:
		a.Toggle()
	case 2:
		dbg("uscita richiesta dal menu della linguetta")
		a.Quit()
	}
}

func (a *App) paintTab(hwnd windows.Handle) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	rc := clientRect(hwnd)

	base, ink := a.tabColors()
	brush, _, _ := pCreateSolidBrush.Call(base)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	pDeleteObject.Call(brush)

	// freccia
	if a.font == 0 {
		sc := a.geo.scale
		if sc <= 0 {
			sc = 1
		}
		h := int32(21*sc + 0.5)
		f, _, _ := pCreateFontW.Call(uintptr(h), 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, cleartypeQualty, 0,
			uintptr(unsafe.Pointer(utf16("Segoe UI"))))
		a.font = windows.Handle(f)
	}
	old, _, _ := pSelectObject.Call(hdc, uintptr(a.font))
	pSetBkMode.Call(hdc, transparentBk)
	pSetTextColor.Call(hdc, ink)

	glyph := "‹"
	if a.geo.right == a.open {
		glyph = "›"
	}
	txt := utf16(glyph)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(txt)), ^uintptr(0),
		uintptr(unsafe.Pointer(&rc)), dtCenter|dtVCenter|dtSingleLine|dtNoClip)
	pSelectObject.Call(hdc, old)

	pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
}

// paintWarning avvisa l'utente quando l'interfaccia non si carica,
// invece di lasciare il pannello vuoto.
func (a *App) paintWarning(hdc uintptr, rc rect) {
	sc := a.geo.scale
	if sc <= 0 {
		sc = 1
	}
	f, _, _ := pCreateFontW.Call(uintptr(int32(15*sc+0.5)), 0, 0, 0, 500, 0, 0, 0, 1, 0, 0,
		cleartypeQualty, 0, uintptr(unsafe.Pointer(utf16("Segoe UI"))))
	old, _, _ := pSelectObject.Call(hdc, f)
	pSetBkMode.Call(hdc, transparentBk)
	if a.darkTheme() {
		pSetTextColor.Call(hdc, rgb(0xE0, 0xE0, 0xE6))
	} else {
		pSetTextColor.Call(hdc, rgb(0x40, 0x40, 0x48))
	}
	box := rect{Left: rc.Left + int32(24*sc), Top: rc.Top + int32(60*sc),
		Right: rc.Right - int32(24*sc), Bottom: rc.Top + int32(320*sc)}
	msg := "UdiAgenda non è riuscito a caricare l'interfaccia.\r\n\r\n" +
		"Serve il componente \"Microsoft Edge WebView2\", normalmente già presente in Windows 11.\r\n\r\n" +
		"Nella cartella dei dati (%LOCALAPPDATA%\\UdiAgenda) trovi il file log.txt con il dettaglio: " +
		"inviamelo e sistemo il problema."
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16(msg))), ^uintptr(0),
		uintptr(unsafe.Pointer(&box)), 0x0010|dtCenter) // DT_WORDBREAK | DT_CENTER
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(f)
}

// ------------------------------------------------------------ finestra pannello

func panelProc(hwnd, msg, wp, lp uintptr) uintptr {
	a := app
	if a == nil {
		return def(hwnd, msg, wp, lp)
	}
	switch msg {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		rc := clientRect(windows.Handle(hwnd))
		col := rgb(0xF7, 0xF7, 0xFA)
		if a.darkTheme() {
			col = rgb(0x20, 0x20, 0x24)
		}
		br, _, _ := pCreateSolidBrush.Call(col)
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), br)
		pDeleteObject.Call(br)
		if a.showWarn {
			a.paintWarning(hdc, rc)
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmTimer:
		if wp == timerAnim {
			a.stepAnim()
			return 0
		}
		if wp == timerWarm {
			pKillTimer.Call(hwnd, timerWarm)
			if !a.jsAlive {
				if a.chromium != nil {
					dbg("la pagina non risponde: ricarico come documento unico")
					a.chromium.Resize()
					_ = a.chromium.Show()
					a.chromium.NavigateToString(ui.InlineHTML())
				}
				pSetTimer.Call(hwnd, timerWarn, warnMillis, 0)
			}
			if !a.open && !a.animating {
				showWindow(windows.Handle(hwnd), swHide)
			}
			return 0
		}
		if wp == timerWarn {
			pKillTimer.Call(hwnd, timerWarn)
			if !a.jsAlive {
				dbg("interfaccia non caricata: mostro il messaggio di avviso")
				a.showWarn = true
				invalidate(windows.Handle(hwnd))
			}
			return 0
		}
	case wmSize:
		a.resizeWebView()
		return 0
	case wmMove:
		if a.chromium != nil {
			_ = a.chromium.NotifyParentWindowPositionChanged()
		}
		return 0
	case wmActivate:
		dbg("pannello WM_ACTIVATE wp=%d open=%v modal=%v", wp&0xFFFF, a.open, a.modal)
		if wp&0xFFFF == waInactive && a.open && !a.modal && time.Since(a.openedAt) > 150*time.Millisecond {
			a.dismissed = true
			a.Close()
		}
		return 0
	case wmClose:
		a.Close()
		return 0
	case wmDpiChanged, wmSettingChange, wmDisplayChange:
		if !a.animating {
			a.applyGeometry()
		}
		return 0
	case wmDestroy:
		if !a.quitting {
			pPostQuitMessage.Call(0)
		}
		return 0
	}
	return def(hwnd, msg, wp, lp)
}
