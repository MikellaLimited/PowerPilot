//go:build windows

package main

import (
	"math"
	"unsafe"
)

// uiScaleFactor040 is the single presentation-scale source for the normal window.
// The compact mini window intentionally stays at 100% so its fixed footprint remains predictable.

func miniScalePercent040() int {
	v := app.settings.MiniSize
	switch v {
	case 90, 100, 120:
		return v
	default:
		return 100
	}
}

func miniClientSize040() (int32, int32) {
	s := float64(miniScalePercent040()) / 100.0
	// Width follows the selected compact/normal/large preset. Height keeps a
	// content-safe minimum so two metric rows and the progress bar never collide
	// with the action buttons in compact mode.
	height := int32(math.Round(float64(miniClientH) * s))
	if height < miniClientH {
		height = miniClientH
	}
	return int32(math.Round(float64(miniClientW) * s)), height
}

func uiScaleFactor040() float64 {
	if app.miniMode {
		return 1
	}
	v := app.settings.UIScale
	if v == 0 {
		v = 100
	}
	v = clampInt(v, 90, 125)
	return float64(v) / 100.0
}

func scaledInt040(v int) int {
	return int(math.Round(float64(v) * uiScaleFactor040()))
}
func unscaledInt040(v int32) int32 {
	s := uiScaleFactor040()
	if s == 0 {
		return v
	}
	return int32(math.Round(float64(v) / s))
}
func logicalClientRect040(physical RECT) RECT {
	if app.miniMode {
		return physical
	}
	s := uiScaleFactor040()
	return RECT{0, 0, int32(math.Floor(float64(physical.Right-physical.Left) / s)), int32(math.Floor(float64(physical.Bottom-physical.Top) / s))}
}
func clientPointToLogical040(x, y int32) (int32, int32) {
	if app.miniMode {
		return x, y
	}
	return unscaledInt040(x), unscaledInt040(y)
}
func normalMinPhysical040() (int32, int32) {
	s := uiScaleFactor040()
	return int32(math.Round(normalMinClientW * s)), int32(math.Round(normalMinClientH * s))
}
func scaleRectToPhysical040(r RECT) RECT {
	if app.miniMode {
		return r
	}
	s := uiScaleFactor040()
	return RECT{
		Left:   int32(math.Round(float64(r.Left) * s)),
		Top:    int32(math.Round(float64(r.Top) * s)),
		Right:  int32(math.Round(float64(r.Right) * s)),
		Bottom: int32(math.Round(float64(r.Bottom) * s)),
	}
}

func refreshNativeFonts040() {
	sc := uiScaleFactor040()
	newFont := createFont(max(9, int(15*sc+.5)), 400)
	newSmall := createFont(max(8, int(13*sc+.5)), 400)
	newLarge := createFont(max(12, int(20*sc+.5)), 600)
	newInline := createFont(max(8, int(12*sc+.5)), 600)
	app.font, app.fontSmall, app.fontLarge, app.inlineFont = newFont, newSmall, newLarge, newInline
	inline := map[int]bool{idSafetyIdle: true, idWakeLead: true, idWarning: true, idIdleMinutes: true}
	textish := map[int]bool{idExact: true, idWatchProcess: true, idScheduleTime: true, idCondThreshold: true, idCondText: true, idStepText: true, idSavedSearch: true, idHistorySearch: true, idTaskName: true}
	for id, h := range app.edits {
		f := app.fontSmall
		if inline[id] {
			f = app.inlineFont
		} else if textish[id] {
			f = app.font
		}
		pSendMessageW.Call(h, WM_SETFONT, f, 1)
	}
}

func applyUIScaleChange040(newScale int) {
	newScale = clampInt(newScale, 90, 125)
	if app.settings.UIScale == newScale {
		return
	}
	app.settings.UIScale = newScale
	refreshNativeFonts040()
	saveSettings()
	if app.hwnd == 0 || app.miniMode {
		return
	}
	mw, mh := normalMinPhysical040()
	var cr RECT
	pGetClientRect.Call(app.hwnd, uintptr(unsafe.Pointer(&cr)))
	cw, ch := int32(cr.Right-cr.Left), int32(cr.Bottom-cr.Top)
	// Never let a larger UI scale make the existing window smaller than the logical minimum.
	if cw < mw || ch < mh || app.settings.LockMinimumSize {
		var wr RECT
		pGetWindowRect.Call(app.hwnd, uintptr(unsafe.Pointer(&wr)))
		nw, nh := max32(cw, mw), max32(ch, mh)
		pSetWindowPos.Call(app.hwnd, 0, uintptr(wr.Left), uintptr(wr.Top), uintptr(nw), uintptr(nh), SWP_NOZORDER|SWP_NOACTIVATE)
	}
	layoutControls(app.hwnd)
	invalidate(app.hwnd)
}
