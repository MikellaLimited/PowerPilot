//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

// UISystem is the single source of truth for visual spacing and compact form controls.
// Pages should compose their geometry from these tokens/helpers instead of inventing local
// magic offsets. This keeps the Simple task, Advanced task, Settings and Saved-task editor
// visually consistent at every supported window size.
type uiMetrics struct {
	PagePad          int
	InnerPad         int
	GapXS            int
	GapS             int
	GapM             int
	GapL             int
	SectionGap       int
	LabelGap         int
	RowHeight        int
	SettingsRowStep  int
	ButtonHeight     int
	FieldHeight      int
	CompactFieldH    int
	NativeEditH      int
	InlineFontSize   int
	InlineFontWeight int
}

var uiMetricsDefault = uiMetrics{
	PagePad:          20,
	InnerPad:         18,
	GapXS:            3,
	GapS:             4,
	GapM:             10,
	GapL:             14,
	SectionGap:       18,
	LabelGap:         6,
	RowHeight:        28,
	SettingsRowStep:  48,
	ButtonHeight:     38,
	FieldHeight:      30,
	CompactFieldH:    20,
	NativeEditH:      20,
	InlineFontSize:   12,
	InlineFontWeight: 600,
}

var (
	uiGetDC                 = user32.NewProc("GetDC")
	uiReleaseDC             = user32.NewProc("ReleaseDC")
	uiGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
	uiMeasureMu             sync.Mutex
	uiMeasureCache          = map[uiMeasureKey]int{}
)

type uiMeasureKey struct {
	Text   string
	Size   int
	Weight int
}

type uiSize struct{ CX, CY int32 }

// uiTextWidth measures Segoe UI using the same Win32 font cache that backs native EDITs.
// DirectWrite's natural metrics are close enough that this is stable for layout while keeping
// measurement cheap and independent of the current render target.
func uiTextWidth(text string, size, weight int) int {
	if text == "" {
		return 0
	}
	key := uiMeasureKey{text, size, weight}
	uiMeasureMu.Lock()
	if v, ok := uiMeasureCache[key]; ok {
		uiMeasureMu.Unlock()
		return v
	}
	uiMeasureMu.Unlock()

	// DirectWrite renders the application, so DirectWrite must also be the primary
	// measurement engine. GDI remains only as a startup/failure fallback.
	w := dwriteMeasureTextWidth(text, size, weight)
	if w < 1 {
		hdc, _, _ := uiGetDC.Call(app.hwnd)
		if hdc != 0 {
			font := createFont(size, weight)
			old, _, _ := pSelectObject.Call(hdc, font)
			u := syscall.StringToUTF16(text)
			sz := uiSize{}
			if len(u) > 1 {
				uiGetTextExtentPoint32W.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
			}
			pSelectObject.Call(hdc, old)
			uiReleaseDC.Call(app.hwnd, hdc)
			w = int(sz.CX)
		}
	}
	if w < 1 {
		w = len([]rune(text)) * max(5, size/2+1)
	}
	uiMeasureMu.Lock()
	uiMeasureCache[key] = w
	uiMeasureMu.Unlock()
	return w
}

func uiMaxBottom(rects []RECT) int {
	b := 0
	for _, r := range rects {
		if int(r.Bottom) > b {
			b = int(r.Bottom)
		}
	}
	return b
}

// uiFormTop returns the first y coordinate for content underneath a button grid.
func uiFormTop(rects []RECT) int {
	b := uiMaxBottom(rects)
	if b == 0 {
		return 0
	}
	return b + uiMetricsDefault.GapM
}

func uiCompactFieldRect(x, y, width int) RECT {
	if width < 28 {
		width = 28
	}
	return RECT{int32(x), int32(y), int32(x + width), int32(y + uiMetricsDefault.CompactFieldH)}
}

func uiNumberWidth(digits int) int {
	if digits < 1 {
		digits = 1
	}
	sample := ""
	for i := 0; i < digits; i++ {
		sample += "8"
	}
	return max(24, uiTextWidth(sample, uiMetricsDefault.InlineFontSize, uiMetricsDefault.InlineFontWeight)+8)
}

func uiPlaceEdit(id int, outer RECT, horizontalPad int) {
	h := app.edits[id]
	if h == 0 || outer.Right <= outer.Left || outer.Bottom <= outer.Top {
		return
	}
	if horizontalPad < 2 {
		horizontalPad = 2
	}
	hgt := uiMetricsDefault.NativeEditH
	oy := int(outer.Top) + (int(outer.Bottom-outer.Top)-hgt)/2
	move(h, int(outer.Left)+horizontalPad, oy, max(8, int(outer.Right-outer.Left)-horizontalPad*2), hgt)
	pShowWindow.Call(h, SW_SHOW)
}

func uiPlaceInlineNumberEdit(id int, outer RECT) { uiPlaceEdit(id, outer, 2) }
func uiPlaceCompactEdit(id int, outer RECT)      { uiPlaceEdit(id, outer, 4) }

// uiInlineNumberLayout composes one sentence: prefix [number] suffix.
// It deliberately measures both text fragments instead of allocating fixed widths, preventing
// clipping and ensuring one- or two-character spacing around the inline numeric control.
func uiInlineNumberLayout(prefix, suffix string, x, y, right, digits int) (RECT, RECT, RECT) {
	m := uiMetricsDefault
	prefixW := uiTextWidth(prefix, m.InlineFontSize, m.InlineFontWeight)
	suffixW := uiTextWidth(suffix, m.InlineFontSize, m.InlineFontWeight)
	fieldW := uiNumberWidth(digits)
	gap := m.GapS
	total := prefixW + gap + fieldW
	if suffix != "" {
		total += gap + suffixW
	}
	// Never fake consistency by clipping the sentence. The caller is responsible for
	// providing enough row width; the layout keeps the measured text and exact gaps intact.
	_ = total
	pr := RECT{int32(x), int32(y), int32(x + prefixW), int32(y + m.CompactFieldH)}
	fx := x + prefixW + gap
	fr := uiCompactFieldRect(fx, y, fieldW)
	sr := RECT{}
	if suffix != "" {
		sx := int(fr.Right) + gap
		sr = RECT{int32(sx), int32(y), int32(minInt(right, sx+suffixW)), int32(y + m.CompactFieldH)}
	}
	return pr, fr, sr
}

func uiDrawInlineNumber(hdc uintptr, prefix, suffix string, prefixRect, fieldRect, suffixRect RECT) {
	m := uiMetricsDefault
	drawText(hdc, prefix, int(prefixRect.Left), int(prefixRect.Top), int(prefixRect.Right-prefixRect.Left), int(prefixRect.Bottom-prefixRect.Top), m.InlineFontSize, m.InlineFontWeight, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	roundFill(hdc, fieldRect, surfaceButtonColor(), 7)
	if suffix != "" && suffixRect.Right > suffixRect.Left {
		drawText(hdc, suffix, int(suffixRect.Left), int(suffixRect.Top), int(suffixRect.Right-suffixRect.Left), int(suffixRect.Bottom-suffixRect.Top), m.InlineFontSize, m.InlineFontWeight, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
}

// uiFieldRowTop is the canonical vertical placement for form content under a section label.
func uiFieldRowTop(labelY int) int { return labelY + uiMetricsDefault.RowHeight }

// uiSettingsRowTop is the only vertical cadence used by toggle-style settings pages.
func uiSettingsRowTop(contentY, index int) int {
	return contentY + index*uiMetricsDefault.SettingsRowStep
}

// uiInlineSentenceY vertically centers a compact sentence/control inside a 28px toggle row.
func uiInlineSentenceY(rowTop int) int {
	return rowTop + (28-uiMetricsDefault.CompactFieldH)/2
}

// uiResetFieldRects prevents stale native EDIT geometry from leaking across pages.
func uiResetFieldRects() {
	app.whenFieldRect = RECT{}
	app.warningFieldRect = RECT{}
	app.warningFieldRect = RECT{}
	app.pickRect = RECT{}
	app.processClearRect = RECT{}
	for i := range app.timeFieldRects {
		app.timeFieldRects[i] = RECT{}
	}
	for i := range app.exactFieldRects {
		app.exactFieldRects[i] = RECT{}
	}
}

// uiLayoutWhenFields is shared by Simple and Advanced "When" editors. It is intentionally
// the only place that decides the vertical distance between the mode grid, labels and fields.
func uiLayoutWhenFields(mode int, modeRects []RECT, innerLeft, innerRight int, recurrenceToggle bool) {
	uiLayoutWhenFieldsAt(mode, uiFormTop(modeRects), innerLeft, innerRight, recurrenceToggle)
}

// uiLayoutWhenFieldsAt lays out the actual input controls starting at a known form label y.
// Saved-task editing uses this entry point so it gets exactly the same field geometry as the
// Simple and Advanced task editors even though its header structure is different.
func uiLayoutWhenFieldsAt(mode, formY, innerLeft, innerRight int, recurrenceToggle bool) {
	uiResetFieldRects()
	for i := range app.recurrenceKindRects {
		app.recurrenceKindRects[i] = RECT{}
	}
	for i := range app.recurrenceDayRects {
		app.recurrenceDayRects[i] = RECT{}
	}
	app.recurrenceEnabledRect = RECT{}
	if formY == 0 {
		return
	}
	innerContentW := max(1, innerRight-innerLeft)
	m := uiMetricsDefault
	switch mode {
	case 0:
		fieldTop := formY + 34
		fieldGap := m.GapM
		fieldW := 78
		if innerContentW < fieldW*3+fieldGap*2 {
			fieldW = max(54, (innerContentW-fieldGap*2)/3)
		}
		ids := []int{idDelayHours, idDelayMinutes, idDelaySeconds}
		for i, id := range ids {
			x := innerLeft + i*(fieldW+fieldGap)
			app.timeFieldRects[i] = RECT{int32(x), int32(fieldTop), int32(x + fieldW), int32(fieldTop + m.FieldHeight)}
			uiPlaceCompactEdit(id, app.timeFieldRects[i])
		}
	case 1:
		fieldTop := formY + 34
		widths := []int{54, 54, 76, 54, 54}
		gap := m.GapM
		total := gap * 4
		for _, v := range widths {
			total += v
		}
		if total > innerContentW {
			scale := float64(innerContentW-gap*4) / float64(54+54+76+54+54)
			for i := range widths {
				widths[i] = max(46, int(float64(widths[i])*scale))
			}
		}
		x := innerLeft
		ids := []int{idExactDay, idExactMonth, idExactYear, idExactHour, idExactMinute}
		for i, id := range ids {
			app.exactFieldRects[i] = RECT{int32(x), int32(fieldTop), int32(x + widths[i]), int32(fieldTop + m.FieldHeight)}
			uiPlaceCompactEdit(id, app.exactFieldRects[i])
			x += widths[i] + gap
		}
	case 2:
		fieldTop := formY + 24
		fw := 54
		app.whenFieldRect = RECT{int32(innerLeft), int32(fieldTop), int32(innerLeft + fw), int32(fieldTop + m.FieldHeight)}
		uiPlaceCompactEdit(idIdleMinutes, app.whenFieldRect)
	case 3:
		fieldTop := formY + 24
		fw := minInt(270, max(150, innerContentW-250))
		app.whenFieldRect = RECT{int32(innerLeft), int32(fieldTop), int32(innerLeft + fw), int32(fieldTop + m.FieldHeight)}
		uiPlaceCompactEdit(idWatchProcess, app.whenFieldRect)
		pickLeft := innerLeft + fw + m.GapM
		clearW := 32
		pickRight := minInt(innerRight-clearW-m.GapS, pickLeft+170)
		if pickRight < pickLeft+96 {
			pickRight = minInt(innerRight-clearW-m.GapS, pickLeft+96)
		}
		app.pickRect = RECT{int32(pickLeft), int32(fieldTop - 4), int32(pickRight), int32(fieldTop + m.FieldHeight + 4)}
		app.processClearRect = RECT{int32(pickRight + m.GapS), int32(fieldTop - 2), int32(minInt(innerRight, pickRight+m.GapS+clearW)), int32(fieldTop + m.FieldHeight + 2)}
	case 4:
		fieldTop := formY + 22
		fw := 104
		app.whenFieldRect = RECT{int32(innerLeft), int32(fieldTop), int32(innerLeft + fw), int32(fieldTop + m.FieldHeight)}
		uiPlaceCompactEdit(idScheduleTime, app.whenFieldRect)
		kindX := innerLeft + fw + m.GapM
		kw := max(82, (innerRight-kindX-16)/3)
		for i := 0; i < 3; i++ {
			x := kindX + i*(kw+6)
			app.recurrenceKindRects[i] = RECT{int32(x), int32(fieldTop - 2), int32(x + kw), int32(fieldTop + 34)}
		}
		dayY := fieldTop + 44
		dayGap := 6
		dayW := (innerContentW - dayGap*6) / 7
		for i := 0; i < 7; i++ {
			x := innerLeft + i*(dayW+dayGap)
			app.recurrenceDayRects[i] = RECT{int32(x), int32(dayY), int32(x + dayW), int32(dayY + 30)}
		}
		if recurrenceToggle {
			app.recurrenceEnabledRect = RECT{int32(innerLeft), int32(dayY + 40), int32(innerLeft + 28), int32(dayY + 68)}
		}
	case 5:
		// "By conditions" intentionally has no input field.
	}
}

// uiDrawWhenFieldChrome draws the common labels/frames shared by every When editor.
func uiDrawWhenFieldChrome(hdc uintptr, mode int, modeRects []RECT, body RECT, title string) {
	formY := uiFormTop(modeRects)
	if formY == 0 {
		return
	}
	if title != "" {
		drawText(hdc, title, int(body.Left)+18, formY, int(body.Right-body.Left)-36, 16, 11, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	}
	switch mode {
	case 0:
		labels := []string{"Часы", "Минуты", "Секунды"}
		for i, r := range app.timeFieldRects {
			if r.Right <= r.Left {
				continue
			}
			roundFill(hdc, r, surfaceButtonColor(), 8)
			drawText(hdc, labels[i], int(r.Left), int(r.Top)-15, int(r.Right-r.Left), 13, 9, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}
	case 1:
		labels := []string{"День", "Месяц", "Год", "Часы", "Минуты"}
		for i, r := range app.exactFieldRects {
			if r.Right <= r.Left {
				continue
			}
			roundFill(hdc, r, surfaceButtonColor(), 8)
			drawText(hdc, labels[i], int(r.Left), int(r.Top)-15, int(r.Right-r.Left), 13, 9, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}
		pairs := [][2]int{{0, 1}, {1, 2}}
		for _, p := range pairs {
			a, b := app.exactFieldRects[p[0]], app.exactFieldRects[p[1]]
			drawText(hdc, ".", int(a.Right), int(a.Top), int(b.Left-a.Right), int(a.Bottom-a.Top), 14, 650, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		}
		a, b := app.exactFieldRects[2], app.exactFieldRects[3]
		drawText(hdc, "·", int(a.Right), int(a.Top), int(b.Left-a.Right), int(a.Bottom-a.Top), 12, 650, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		a, b = app.exactFieldRects[3], app.exactFieldRects[4]
		drawText(hdc, ":", int(a.Right), int(a.Top), int(b.Left-a.Right), int(a.Bottom-a.Top), 13, 650, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	case 2, 3, 4:
		if app.whenFieldRect.Right > app.whenFieldRect.Left {
			roundFill(hdc, app.whenFieldRect, surfaceButtonColor(), 8)
		}
	}
}

func uiWhenContentBottom(formY int) int {
	b := formY + uiMetricsDefault.CompactFieldH
	for _, r := range app.timeFieldRects {
		if int(r.Bottom) > b {
			b = int(r.Bottom)
		}
	}
	for _, r := range app.exactFieldRects {
		if int(r.Bottom) > b {
			b = int(r.Bottom)
		}
	}
	if int(app.whenFieldRect.Bottom) > b {
		b = int(app.whenFieldRect.Bottom)
	}
	for _, r := range app.recurrenceKindRects {
		if int(r.Bottom) > b {
			b = int(r.Bottom)
		}
	}
	for _, r := range app.recurrenceDayRects {
		if int(r.Bottom) > b {
			b = int(r.Bottom)
		}
	}
	if int(app.recurrenceEnabledRect.Bottom) > b {
		b = int(app.recurrenceEnabledRect.Bottom)
	}
	return b
}
