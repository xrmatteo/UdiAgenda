//go:build windows

// Package winui contiene tutto il codice specifico di Windows:
// la linguetta sul bordo dello schermo, il pannello a scomparsa con
// WebView2, l'avvio automatico e la lettura del tema di sistema.
package winui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pShowWindow           = user32.NewProc("ShowWindow")
	pSetWindowPos         = user32.NewProc("SetWindowPos")
	pGetMessageW          = user32.NewProc("GetMessageW")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pPostQuitMessage      = user32.NewProc("PostQuitMessage")
	pPostMessageW         = user32.NewProc("PostMessageW")
	pLoadCursorW          = user32.NewProc("LoadCursorW")
	pSetTimer             = user32.NewProc("SetTimer")
	pKillTimer            = user32.NewProc("KillTimer")
	pSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	pBeginPaint           = user32.NewProc("BeginPaint")
	pEndPaint             = user32.NewProc("EndPaint")
	pFillRect             = user32.NewProc("FillRect")
	pInvalidateRect       = user32.NewProc("InvalidateRect")
	pTrackMouseEvent      = user32.NewProc("TrackMouseEvent")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pIsWindowVisible      = user32.NewProc("IsWindowVisible")
	pDrawTextW            = user32.NewProc("DrawTextW")
	pMessageBoxW          = user32.NewProc("MessageBoxW")
	pFindWindowW          = user32.NewProc("FindWindowW")
	pGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	pCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	pAppendMenuW          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	pDestroyMenu          = user32.NewProc("DestroyMenu")
	pGetCursorPos         = user32.NewProc("GetCursorPos")
	pSetLayeredWindowAttr = user32.NewProc("SetLayeredWindowAttributes")
	pSetProcessDpiCtx     = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware   = user32.NewProc("SetProcessDPIAware")

	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pCreateFontW      = gdi32.NewProc("CreateFontW")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkMode        = gdi32.NewProc("SetBkMode")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")
)

// Messaggi e costanti Win32 usate.
const (
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmActivate      = 0x0006
	wmPaint         = 0x000F
	wmClose         = 0x0010
	wmSettingChange = 0x001A
	wmTimer         = 0x0113
	wmMouseMove     = 0x0200
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmMouseLeave    = 0x02A3
	wmDisplayChange = 0x007E
	wmDpiChanged    = 0x02E0
	wmMove          = 0x0003
	wmApp           = 0x8000

	waInactive = 0

	wsPopup      = 0x80000000
	wsVisible    = 0x10000000
	wsExTopmost  = 0x00000008
	wsExToolWin  = 0x00000080
	wsExNoActive = 0x08000000
	wsExLayered  = 0x00080000
	lwaAlpha     = 0x00000002

	swHide         = 0
	swShow         = 5
	swShowNoActive = 4

	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpShowWindow = 0x0040

	hwndTopmost = ^uintptr(0) // (HWND)-1

	spiGetWorkArea = 0x0030

	idcArrow = 32512
	idcHand  = 32649

	transparentBk = 1

	dtCenter        = 0x00000001
	dtVCenter       = 0x00000004
	dtSingleLine    = 0x00000020
	dtNoClip        = 0x00000100
	tmeLeave        = 0x00000002
	dwmCornerAttr   = 33
	dwmCornerRound  = 2
	dwmCornerSmall  = 3
	cleartypeQualty = 5

	messageOpenPanel = wmApp + 1

	mfString      = 0x0000
	mfSeparator   = 0x0800
	tpmLeftAlign  = 0x0000
	tpmRightAlign = 0x0008
	tpmRightBtn   = 0x0002
	tpmReturnCmd  = 0x0100
	tpmNoAnimate  = 0x4000
)

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) width() int32  { return r.Right - r.Left }
func (r rect) height() int32 { return r.Bottom - r.Top }

type point struct{ X, Y int32 }

type msgStruct struct {
	Hwnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type paintStruct struct {
	Hdc         windows.Handle
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type trackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   windows.Handle
	DwHoverTime uint32
}

func utf16(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("")
	}
	return p
}

func moduleHandle() windows.Handle {
	h, _, _ := pGetModuleHandleW.Call(0)
	return windows.Handle(h)
}

// rgb costruisce un COLORREF (0x00BBGGRR).
func rgb(r, g, b uint32) uintptr { return uintptr(r | g<<8 | b<<16) }

func setWindowPos(hwnd windows.Handle, after uintptr, x, y, w, h int32, flags uint32) {
	pSetWindowPos.Call(uintptr(hwnd), after, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
}

func showWindow(hwnd windows.Handle, cmd int32) {
	pShowWindow.Call(uintptr(hwnd), uintptr(cmd))
}

func isVisible(hwnd windows.Handle) bool {
	r, _, _ := pIsWindowVisible.Call(uintptr(hwnd))
	return r != 0
}

func invalidate(hwnd windows.Handle) {
	pInvalidateRect.Call(uintptr(hwnd), 0, 1)
}

func clientRect(hwnd windows.Handle) rect {
	var r rect
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return r
}

// workArea restituisce l'area di lavoro del monitor principale
// (lo schermo meno la barra delle applicazioni), in pixel reali.
func workArea() rect {
	var r rect
	ok, _, _ := pSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	if ok == 0 {
		return rect{0, 0, 1920, 1040}
	}
	return r
}

// dpiScale restituisce il fattore di scala di Windows (1.0 = 100%, 1.5 = 150%).
func dpiScale(hwnd windows.Handle) float64 {
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(uintptr(hwnd)); d >= 48 {
			return float64(d) / 96.0
		}
	}
	return 1.0
}

// enableDPIAwareness va richiamata prima di creare finestre.
func enableDPIAwareness() {
	if pSetProcessDpiCtx.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		if r, _, _ := pSetProcessDpiCtx.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	pSetProcessDPIAware.Call()
}

// roundCorners applica gli angoli arrotondati di Windows 11 (ignorato altrove).
func roundCorners(hwnd windows.Handle, pref uint32) {
	if pDwmSetWindowAttribute.Find() != nil {
		return
	}
	p := pref
	pDwmSetWindowAttribute.Call(uintptr(hwnd), dwmCornerAttr, uintptr(unsafe.Pointer(&p)), unsafe.Sizeof(p))
}

func messageBox(title, text string) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(title))), 0x40)
}

func clampInt32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
