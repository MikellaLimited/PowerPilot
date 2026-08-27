//go:build windows

package main

import (
	"fmt"
	"math"
	"time"
)

func layoutResourceMonitor(bodyTop, bodyBottom, innerLeft, innerRight int) {
	// Refresh controls sit on the same line as the module heading.
	labelsW := []int{50, 50, 52, 48, 48}
	total := 0
	for _, v := range labelsW {
		total += v
	}
	total += 6 * (len(labelsW) - 1)
	x := innerRight - total
	y := bodyTop + 16
	for i, w := range labelsW {
		app.resourceRefreshRects[i] = RECT{int32(x), int32(y), int32(x + w), int32(y + 30)}
		x += w + 6
	}

	cardY := bodyTop + 62
	gap := 10
	cardW := (innerRight - innerLeft - gap*2) / 3
	cardH := 72
	for i := 0; i < len(app.resourceCardRects); i++ {
		row, col := i/3, i%3
		x := innerLeft + col*(cardW+gap)
		yy := cardY + row*(cardH+gap)
		app.resourceCardRects[i] = RECT{int32(x), int32(yy), int32(x + cardW), int32(yy + cardH)}
	}
	graphTop := cardY + cardH*2 + gap + 18
	app.resourceGraphRect = RECT{int32(innerLeft), int32(graphTop), int32(innerRight), int32(max(graphTop+170, bodyBottom-18))}
	drives := metricsSnapshot().Disks
	app.resourceDiskRects = make([]RECT, len(drives))
	dx, dy := innerLeft+14, graphTop+36
	for i := range drives {
		pillW := max(52, uiTextWidth(drives[i].Name, 10, 600)+22)
		if dx+pillW > innerRight-14 && dx > innerLeft+14 {
			dx = innerLeft + 14
			dy += 34
		}
		app.resourceDiskRects[i] = RECT{int32(dx), int32(dy), int32(dx + pillW), int32(dy + 28)}
		dx += pillW + 6
	}
}

func resourceRefreshIndex(ms int) int {
	vals := []int{250, 500, 1000, 2000, 5000}
	best, diff := 0, math.MaxInt
	for i, v := range vals {
		d := v - ms
		if d < 0 {
			d = -d
		}
		if d < diff {
			diff, best = d, i
		}
	}
	return best
}

func drawResourceMonitor(hdc uintptr, body RECT, w int) {
	snap := metricsSnapshot()
	drawText(hdc, "Ресурсы", int(body.Left)+18, int(body.Top)+12, 250, 30, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "Обновление", int(app.resourceRefreshRects[0].Left)-82, int(app.resourceRefreshRects[0].Top), 74, 30, 10, 500, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	names := []string{"250мс", "500мс", "1с", "2с", "5с"}
	selectedRefresh := resourceRefreshIndex(app.settings.ResourceRefreshMS)
	for i, r := range app.resourceRefreshRects {
		drawSelectableButton(hdc, r, names[i], i == selectedRefresh)
	}

	cards := []struct{ title, value, temp, sub string }{
		{"Процессор", metricPercentText(snap.CPU), averageTemperatureValueText("cpu"), "Текущая загрузка CPU"},
		{"Видеокарта", metricPercentText(snap.GPU), averageTemperatureValueText("gpu"), vramText(snap.VRAMUsedMB)},
		{"Оперативная память", metricPercentText(snap.RAMPercent), "", ramText(snap)},
		{"Диски", metricPercentText(selectedDiskActivity(snap)), averageTemperatureValueText("disk"), diskSpaceText(snap)},
		{"Сеть", "↓ " + formatNetworkRateKB(snap.NetworkDownKBps), "", "↑ " + formatNetworkRateKB(snap.NetworkUpKBps)},
		{"Питание", powerValueText(snap), "", powerSubText(snap)},
	}
	for i, r := range app.resourceCardRects {
		active := app.resourceSelected == i
		c := surfaceButtonColor()
		if active {
			c = blendColor(c, theme.accent, .22)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .07*hv)
		}
		roundFill(hdc, rv, c, 13)
		if hv > 0 && !active && ui2d.active {
			d2dDrawRoundedOutline(rv, 13, float32(1+.35*hv), blendColor(theme.border, theme.accent2, .44))
		}
		drawText(hdc, cards[i].title, int(r.Left)+12, int(r.Top)+7, int(r.Right-r.Left)-24, 18, 10, 600, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		valueW := int(r.Right-r.Left) - 24
		drawText(hdc, cards[i].value, int(r.Left)+12, int(r.Top)+26, valueW, 25, 19, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		if cards[i].temp != "" {
			// Keep the temperature with the primary value on the left side of the card.
			// It reads as one metric cluster instead of a detached badge on the far right.
			tx := int(r.Left) + 18 + uiTextWidth(cards[i].value, 19, 700)
			maxX := int(r.Right) - 62
			if tx > maxX {
				tx = maxX
			}
			drawText(hdc, "ср. "+cards[i].temp, tx, int(r.Top)+27, int(r.Right)-tx-10, 22, 10, 650, theme.accent2, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		}
		drawText(hdc, cards[i].sub, int(r.Left)+12, int(r.Top)+51, int(r.Right-r.Left)-24, 14, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}

	drawResourceGraph(hdc, app.resourceGraphRect, app.resourceSelected)
	if !snap.Updated.IsZero() {
		age := time.Since(snap.Updated)
		txt := "Последнее обновление: сейчас"
		if age > 2*time.Second {
			txt = fmt.Sprintf("Последнее обновление: %.1f с назад", age.Seconds())
		}
		drawText(hdc, txt, int(body.Left)+18, int(body.Bottom)-22, int(body.Right-body.Left)-36, 16, 9, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
}

func averageTemperatureValueText(kind string) string {
	if !temperatureDisplayEnabled() {
		return ""
	}
	v := averageTemperature(kind)
	if v < 0 {
		return "—°C"
	}
	return fmt.Sprintf("%.0f°C", v)
}

func metricPercentText(v float64) string {
	if v < 0 || math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", v)
}
func vramText(v float64) string {
	if v < 0 {
		return "VRAM: недоступно"
	}
	if v >= 1024 {
		return fmt.Sprintf("VRAM: %.1f ГБ", v/1024)
	}
	return fmt.Sprintf("VRAM: %.0f МБ", v)
}
func ramText(s MetricSnapshot) string {
	if s.RAMTotalGB <= 0 {
		return "Физическая память"
	}
	return fmt.Sprintf("%.1f / %.1f ГБ", s.RAMUsedGB, s.RAMTotalGB)
}
func selectedDiskName(s MetricSnapshot) string {
	if len(s.Disks) == 0 {
		return ""
	}
	i := app.resourceDiskSelected
	if i < 0 || i >= len(s.Disks) {
		i = 0
	}
	return s.Disks[i].Name
}
func selectedDiskMetric(s MetricSnapshot) (DiskMetric, bool) {
	if len(s.Disks) == 0 {
		return DiskMetric{}, false
	}
	i := app.resourceDiskSelected
	if i < 0 || i >= len(s.Disks) {
		i = 0
	}
	return s.Disks[i], true
}
func selectedDiskActivity(s MetricSnapshot) float64 {
	if d, ok := selectedDiskMetric(s); ok {
		return d.Activity
	}
	return s.DiskPercent
}
func diskSpaceText(s MetricSnapshot) string {
	d, ok := selectedDiskMetric(s)
	if !ok || d.TotalGB <= 0 || d.FreeGB < 0 {
		return "Доступные диски"
	}
	return fmt.Sprintf("%s свободно %.0f / %.0f ГБ", d.Name, d.FreeGB, d.TotalGB)
}

func powerValueText(s MetricSnapshot) string {
	if s.BatteryPercent >= 0 {
		return fmt.Sprintf("%.0f%%", s.BatteryPercent)
	}
	if s.OnAC {
		return "Сеть"
	}
	return "—"
}
func powerSubText(s MetricSnapshot) string {
	if s.BatteryPercent >= 0 {
		if s.OnAC {
			return "Подключено питание"
		}
		return "Работа от батареи"
	}
	if s.OnAC {
		return "Батарея не обнаружена"
	}
	return "Статус питания недоступен"
}
func formatRateValueKB(kb float64) string {
	if kb < 0 {
		return "—"
	}
	if kb >= 1024 {
		return fmt.Sprintf("%.1f МБ/с", kb/1024)
	}
	return fmt.Sprintf("%.0f КБ/с", kb)
}

func formatNetworkRateKB(kb float64) string {
	return formatNetworkRateKBWithUnit(kb, app.settings.NetworkRateBits)
}

func formatNetworkRateKBWithUnit(kb float64, bits bool) string {
	if kb < 0 || math.IsNaN(kb) || math.IsInf(kb, 0) {
		return "—"
	}
	bytesPerSecond := kb * 1024
	if bits {
		bitsPerSecond := bytesPerSecond * 8
		switch {
		case bitsPerSecond >= 1_000_000_000:
			return fmt.Sprintf("%.1f Гбит/с", bitsPerSecond/1_000_000_000)
		case bitsPerSecond >= 1_000_000:
			return fmt.Sprintf("%.1f Мбит/с", bitsPerSecond/1_000_000)
		case bitsPerSecond >= 1_000:
			return fmt.Sprintf("%.0f Кбит/с", bitsPerSecond/1_000)
		default:
			return fmt.Sprintf("%.0f бит/с", bitsPerSecond)
		}
	}
	switch {
	case bytesPerSecond >= 1024*1024*1024:
		return fmt.Sprintf("%.1f ГБ/с", bytesPerSecond/(1024*1024*1024))
	case bytesPerSecond >= 1024*1024:
		return fmt.Sprintf("%.1f МБ/с", bytesPerSecond/(1024*1024))
	case bytesPerSecond >= 1024:
		return fmt.Sprintf("%.0f КБ/с", bytesPerSecond/1024)
	default:
		return fmt.Sprintf("%.0f Б/с", bytesPerSecond)
	}
}

func graphMetricValue(s MetricSnapshot, kind int) float64 {
	switch kind {
	case 0:
		return s.CPU
	case 1:
		return s.GPU
	case 2:
		return s.RAMPercent
	case 3:
		return selectedDiskActivity(s)
	case 4:
		return s.NetworkDownKBps + s.NetworkUpKBps
	case 5:
		return s.BatteryPercent
	}
	return 0
}
func graphName(kind int) string {
	n := []string{"Процессор", "Видеокарта", "Оперативная память", "Диски", "Сеть", "Батарея"}
	if kind == 3 {
		if nme := selectedDiskName(metricsSnapshot()); nme != "" {
			return "Диск " + nme
		}
	}
	if kind < 0 || kind >= len(n) {
		return "Ресурс"
	}
	return n[kind]
}
func graphValueText(kind int, v float64) string {
	if v < 0 {
		return "—"
	}
	if kind == 4 {
		return formatNetworkRateKB(v)
	}
	return fmt.Sprintf("%.0f%%", v)
}

func drawResourceGraph(hdc uintptr, r RECT, kind int) {
	if r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	roundFill(hdc, r, blendColor(surfacePanelColor(), surfaceButtonColor(), .42), 14)
	hist := metricHistory()
	if len(hist) > 120 {
		hist = hist[len(hist)-120:]
	}
	vals := make([]float64, 0, len(hist))
	for _, s := range hist {
		v := graphMetricValue(s, kind)
		if v >= 0 && !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	current := -1.0
	avg, maxV := 0.0, 0.0
	if len(vals) > 0 {
		current = vals[len(vals)-1]
		for _, v := range vals {
			avg += v
			if v > maxV {
				maxV = v
			}
		}
		avg /= float64(len(vals))
	}
	drawText(hdc, graphName(kind), int(r.Left)+14, int(r.Top)+8, 180, 22, 13, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	stat := fmt.Sprintf("Сейчас %s   Среднее %s   Макс. %s", graphValueText(kind, current), graphValueText(kind, avg), graphValueText(kind, maxV))
	drawText(hdc, stat, int(r.Left)+195, int(r.Top)+8, int(r.Right-r.Left)-210, 22, 10, 500, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	plotTop := r.Top + 42
	if kind == 3 {
		snap := metricsSnapshot()
		maxBottom := r.Top + 64
		for i, d := range snap.Disks {
			if i >= len(app.resourceDiskRects) {
				break
			}
			rr := app.resourceDiskRects[i]
			if rr.Right <= rr.Left {
				continue
			}
			drawSelectableButton(hdc, rr, d.Name, i == app.resourceDiskSelected)
			if rr.Bottom > maxBottom {
				maxBottom = rr.Bottom
			}
		}
		plotTop = maxBottom + 10
	}
	axisLabelW := int32(36)
	if kind == 4 {
		axisLabelW = 58
	}
	plot := RECT{r.Left + axisLabelW + 8, plotTop, r.Right - 12, r.Bottom - 36}
	if plot.Right <= plot.Left || plot.Bottom <= plot.Top {
		return
	}
	yMax := 100.0
	if kind == 4 {
		yMax = math.Max(100, maxV*1.15)
	}
	for i := 0; i <= 4; i++ {
		yy := float32(plot.Top) + float32(plot.Bottom-plot.Top)*float32(i)/4
		d2dDrawLine(float32(plot.Left), yy, float32(plot.Right), yy, .55, blendColor(theme.border, theme.muted, .18))
		labelV := yMax * (1 - float64(i)/4)
		drawText(hdc, graphValueText(kind, labelV), int(r.Left)+4, int(yy)-8, int(axisLabelW), 16, 8, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
	if len(vals) < 2 {
		drawText(hdc, "Собираю данные…", int(plot.Left), int(plot.Top), int(plot.Right-plot.Left), int(plot.Bottom-plot.Top), 12, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		return
	}
	if len(hist) >= 2 {
		startAt, endAt := hist[0].Updated, hist[len(hist)-1].Updated
		span := endAt.Sub(startAt)
		if span <= 0 {
			span = time.Second
		}
		tickCount := resourceTimelineTickCount()
		for i := 0; i < tickCount; i++ {
			f := float64(i) / float64(tickCount-1)
			tm := startAt.Add(time.Duration(float64(span) * f))
			x := int(plot.Left) + int(float64(plot.Right-plot.Left)*f)
			if i > 0 && i < tickCount-1 {
				d2dDrawLine(float32(x), float32(plot.Top), float32(x), float32(plot.Bottom), .45, blendColor(theme.border, theme.muted, .16))
			}
			flags := uint32(DT_CENTER | DT_VCENTER | DT_SINGLELINE)
			x0, ww := x-42, 84
			if i == 0 {
				flags = DT_LEFT | DT_VCENTER | DT_SINGLELINE
				x0 = int(plot.Left)
				ww = 90
			}
			if i == tickCount-1 {
				flags = DT_RIGHT | DT_VCENTER | DT_SINGLELINE
				x0 = int(plot.Right) - 90
				ww = 90
			}
			drawText(hdc, resourceGraphTimeTick(tm, endAt, span), x0, int(plot.Bottom)+7, ww, 16, 8, 400, theme.muted, flags)
		}
	}
	// Plot every collected point; the history size is bounded, so this stays cheap.
	denom := float64(len(vals) - 1)
	prevX, prevY := float32(plot.Left), float32(plot.Bottom)
	for i, v := range vals {
		x := float32(plot.Left) + float32(float64(plot.Right-plot.Left)*float64(i)/denom)
		frac := v / yMax
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		y := float32(plot.Bottom) - float32(float64(plot.Bottom-plot.Top)*frac)
		if i > 0 {
			d2dDrawLine(prevX, prevY, x, y, 1.8, theme.accent2)
		}
		prevX, prevY = x, y
	}
}
