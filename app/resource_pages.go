//go:build windows

package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func advancedResourceItemCount() int {
	if app.resourceAdvancedView == 1 {
		return len(hardwareSensorDisplayRows())
	}
	return len(filteredResourceProcesses())
}

var resourceSensorCategories = []struct {
	Label string
	Type  string
}{
	{"Все", "**"},
	{"Температуры", "Temperature"},
	{"Напряжения", "Voltage"},
	{"Вентиляторы", "Fan"},
	{"Мощность", "Power"},
	{"Частоты", "Clock"},
	{"Нагрузка", "Load"},
	{"Остальное", "*"},
}

func hardwareSensorIsPrimaryType(t string) bool {
	for _, category := range resourceSensorCategories {
		if category.Type != "*" && category.Type != "**" && strings.EqualFold(t, category.Type) {
			return true
		}
	}
	return false
}

func filteredHardwareSensors() []HardwareSensor {
	all := hardwareSensorsSnapshot()
	if app.resourceSensorView < 0 || app.resourceSensorView >= len(resourceSensorCategories) {
		app.resourceSensorView = 0
	}
	want := resourceSensorCategories[app.resourceSensorView].Type
	out := make([]HardwareSensor, 0, len(all))
	for _, s := range all {
		// Current/amperage readings are intentionally hidden from the PowerPilot sensor UI.
		if strings.EqualFold(s.SensorType, "Current") {
			continue
		}
		if want == "**" {
			// The All view keeps every supported sensor type and adds a second
			// hierarchy level below each hardware component.
		} else if want == "*" {
			if hardwareSensorIsPrimaryType(s.SensorType) {
				continue
			}
		} else if !strings.EqualFold(s.SensorType, want) {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !strings.EqualFold(a.Hardware, b.Hardware) {
			return strings.ToLower(a.Hardware) < strings.ToLower(b.Hardware)
		}
		ka, kb := hardwareSensorComponentKey(a), hardwareSensorComponentKey(b)
		if ka != kb {
			return ka < kb
		}
		if want == "**" && !strings.EqualFold(a.SensorType, b.SensorType) {
			return strings.ToLower(a.SensorType) < strings.ToLower(b.SensorType)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

type hardwareSensorDisplayRow struct {
	GroupKey string
	Hardware string
	Count    int
	Expanded bool
	IsGroup  bool
	Sensor   HardwareSensor
	Level    int
	Label    string
}

func hardwareSensorComponentKey(s HardwareSensor) string {
	id := strings.TrimSpace(strings.ToLower(s.Identifier))
	if id != "" {
		parts := strings.Split(strings.Trim(id, "/"), "/")
		// LibreHardwareMonitor sensor identifiers normally append /<sensor-type>/<index>
		// to the hardware identifier. Removing those two segments gives us a stable
		// component key, including for otherwise identical DIMMs or drives.
		if len(parts) >= 3 {
			return strings.Join(parts[:len(parts)-2], "/")
		}
		return id
	}
	return strings.ToLower(strings.TrimSpace(s.HardwareType + "|" + s.Hardware))
}

func hardwareSensorDisplayRows() []hardwareSensorDisplayRow {
	sensors := filteredHardwareSensors()
	if app.resourceSensorExpanded == nil {
		app.resourceSensorExpanded = map[string]bool{}
	}
	rows := make([]hardwareSensorDisplayRow, 0, len(sensors)+16)
	allView := app.resourceSensorView == 0
	for i := 0; i < len(sensors); {
		key := hardwareSensorComponentKey(sensors[i])
		j := i + 1
		for j < len(sensors) && hardwareSensorComponentKey(sensors[j]) == key {
			j++
		}
		expanded, known := app.resourceSensorExpanded[key]
		if allView && !known {
			expanded = true
			app.resourceSensorExpanded[key] = true
		}
		rows = append(rows, hardwareSensorDisplayRow{GroupKey: key, Hardware: sensors[i].Hardware, Label: sensors[i].Hardware, Count: j - i, Expanded: expanded, IsGroup: true})
		if expanded {
			if allView {
				for k := i; k < j; {
					typeName := sensors[k].SensorType
					m := k + 1
					for m < j && strings.EqualFold(sensors[m].SensorType, typeName) {
						m++
					}
					typeKey := key + "|type:" + strings.ToLower(typeName)
					typeExpanded, typeKnown := app.resourceSensorExpanded[typeKey]
					if !typeKnown {
						typeExpanded = true
						app.resourceSensorExpanded[typeKey] = true
					}
					rows = append(rows, hardwareSensorDisplayRow{GroupKey: typeKey, Hardware: sensors[k].Hardware, Label: hardwareSensorTypeLabel(typeName), Count: m - k, Expanded: typeExpanded, IsGroup: true, Level: 1})
					if typeExpanded {
						for n := k; n < m; n++ {
							rows = append(rows, hardwareSensorDisplayRow{GroupKey: typeKey, Hardware: sensors[n].Hardware, Sensor: sensors[n], Level: 2})
						}
					}
					k = m
				}
			} else {
				for k := i; k < j; k++ {
					rows = append(rows, hardwareSensorDisplayRow{GroupKey: key, Hardware: sensors[k].Hardware, Sensor: sensors[k], Level: 1})
				}
			}
		}
		i = j
	}
	return rows
}

func hardwareSensorTypeLabel(sensorType string) string {
	for _, category := range resourceSensorCategories {
		if category.Type != "*" && category.Type != "**" && strings.EqualFold(category.Type, sensorType) {
			return category.Label
		}
	}
	if strings.TrimSpace(sensorType) == "" {
		return "Другие показатели"
	}
	return sensorType
}

func layoutAdvancedResourceMonitor(bodyTop, bodyBottom, innerLeft, innerRight int) {
	gap := 6
	app.resourceTempProviderRect = RECT{}
	app.resourceTempAdminRect = RECT{}
	for i := range app.resourceAdvancedTabRects {
		app.resourceAdvancedTabRects[i] = RECT{}
	}
	for i := range app.resourceSensorTypeRects {
		app.resourceSensorTypeRects[i] = RECT{}
	}
	tabY := bodyTop + 48
	tabW := min(150, max(118, (innerRight-innerLeft-8)/3))
	for i := 0; i < 2; i++ {
		x := innerLeft + i*(tabW+8)
		app.resourceAdvancedTabRects[i] = RECT{int32(x), int32(tabY), int32(x + tabW), int32(tabY + 32)}
	}
	for i := range app.resourceProcSortRects {
		app.resourceProcSortRects[i] = RECT{}
	}
	clipTop := tabY + 42
	if app.resourceAdvancedView == 0 {
		searchY := tabY + 42
		app.resourceProcessSearchRect = RECT{int32(innerLeft), int32(searchY), int32(innerRight), int32(searchY + 34)}
		move(app.edits[idResourceSearch], innerLeft+8, searchY+3, innerRight-innerLeft-16, 28)
		if app.notificationPanelOpen || app.taskMenuOpen || app.resourceMenuOpen {
			pShowWindow.Call(app.edits[idResourceSearch], SW_HIDE)
		} else {
			pShowWindow.Call(app.edits[idResourceSearch], SW_SHOW)
		}
		y := searchY + 44
		available := innerRight - innerLeft
		nameW := max(150, available*30/100)
		metricW := max(58, (available-nameW-gap*5)/5)
		x := innerLeft
		app.resourceProcSortRects[0] = RECT{int32(x), int32(y), int32(x + nameW), int32(y + 32)}
		x += nameW + gap
		for i := 1; i < len(app.resourceProcSortRects); i++ {
			right := x + metricW
			if i == len(app.resourceProcSortRects)-1 {
				right = innerRight
			}
			app.resourceProcSortRects[i] = RECT{int32(x), int32(y), int32(right), int32(y + 32)}
			x = right + gap
		}
		clipTop = y + 44
	} else {
		app.resourceProcessSearchRect = RECT{}
		pShowWindow.Call(app.edits[idResourceSearch], SW_HIDE)
		// Sensor categories live inside the advanced monitor. Two compact rows keep
		// all major hardware families visible without turning the page into a menu.
		catGap := 6
		cols := 4
		catW := (innerRight - innerLeft - catGap*(cols-1)) / cols
		catY := tabY + 42
		for i := 0; i < len(resourceSensorCategories); i++ {
			row, col := i/cols, i%cols
			x := innerLeft + col*(catW+catGap)
			y := catY + row*36
			app.resourceSensorTypeRects[i] = RECT{int32(x), int32(y), int32(x + catW), int32(y + 30)}
		}
		rows := (len(resourceSensorCategories) + cols - 1) / cols
		clipTop = catY + rows*36 + 30
	}
	clipBottom := bodyBottom - 18
	app.resourceProcListClip = RECT{int32(innerLeft), int32(clipTop), int32(innerRight - 12), int32(clipBottom)}
	app.resourceProcScrollTrack = RECT{int32(innerRight - 7), int32(clipTop), int32(innerRight - 3), int32(clipBottom)}
	stride, rowH := 43, 39
	maxPx := resourceProcessScrollMax()
	app.resourceProcScrollPx = clampFloat(app.resourceProcScrollPx, 0, maxPx)
	app.resourceProcScrollTarget = clampFloat(app.resourceProcScrollTarget, 0, maxPx)
	rem := int(app.resourceProcScrollPx) % stride
	for i := range app.resourceProcRows {
		yy := clipTop - rem + i*stride
		app.resourceProcRows[i] = RECT{int32(innerLeft), int32(yy), int32(innerRight - 12), int32(yy + rowH)}
	}
	contentH := max(0, advancedResourceItemCount()*stride-4)
	viewH := max(1, clipBottom-clipTop)
	app.resourceProcScrollThumb = scrollThumbRectPixels(app.resourceProcScrollTrack, contentH, viewH, app.resourceProcScrollPx)
}

func layoutResourceStatistics(bodyTop, bodyBottom, innerLeft, innerRight int) {
	gap := 6
	y := bodyTop + 54
	for i := range app.resourceStatsPeriodRects {
		app.resourceStatsPeriodRects[i] = RECT{}
	}
	periodCount := 5
	if app.resourceStatsView == 3 || app.resourceStatsView == 4 {
		periodCount = 8
	}
	w := (innerRight - innerLeft - gap*(periodCount-1)) / periodCount
	for i := 0; i < periodCount; i++ {
		x := innerLeft + i*(w+gap)
		app.resourceStatsPeriodRects[i] = RECT{int32(x), int32(y), int32(x + w), int32(y + 32)}
	}
	viewY := y + 40
	viewW := (innerRight - innerLeft - gap*4) / 5
	for i := range app.resourceStatsViewRects {
		x := innerLeft + i*(viewW+gap)
		app.resourceStatsViewRects[i] = RECT{int32(x), int32(viewY), int32(x + viewW), int32(viewY + 32)}
	}
	for i := range app.resourceStatsGraphRects {
		app.resourceStatsGraphRects[i] = RECT{}
	}
	if app.resourceStatsView == 0 || app.resourceStatsView == 1 {
		graphY := viewY + 40
		graphGap := 5
		graphW := (innerRight - innerLeft - graphGap*5) / 6
		for i := range app.resourceStatsGraphRects {
			x := innerLeft + i*(graphW+graphGap)
			app.resourceStatsGraphRects[i] = RECT{int32(x), int32(graphY), int32(x + graphW), int32(graphY + 30)}
		}
	}
}

func resourceProcessScrollMax() float64 {
	clip := app.resourceProcListClip
	viewH := max(1, int(clip.Bottom-clip.Top))
	contentH := max(0, advancedResourceItemCount()*43-4)
	return float64(max(0, contentH-viewH))
}

func filteredResourceProcesses() []ProcessMetric {
	list := processMetricSnapshot()
	out := make([]ProcessMetric, 0, len(list))
	query := strings.ToLower(strings.TrimSpace(app.resourceProcessSearchText))
	for _, p := range list {
		if p.System && !app.settings.ShowSystemProcesses {
			continue
		}
		if app.settings.HideZeroResourceProcesses {
			disk := p.ReadKBps + p.WriteKBps
			if p.CPUPercent <= 0 && p.GPUPercent <= 0 && p.RAMMB <= 0 && disk <= 0 && p.OtherKBps <= 0 {
				continue
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(p.Name), query) && !strings.Contains(strconv.FormatUint(uint64(p.PID), 10), query) {
			continue
		}
		out = append(out, p)
	}
	less := func(a, b ProcessMetric) bool {
		switch app.resourceProcSort {
		case 1:
			if a.CPUPercent == b.CPUPercent {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
			return a.CPUPercent < b.CPUPercent
		case 2:
			if a.GPUPercent == b.GPUPercent {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
			return a.GPUPercent < b.GPUPercent
		case 3:
			if a.RAMMB == b.RAMMB {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
			return a.RAMMB < b.RAMMB
		case 4:
			av, bv := a.ReadKBps+a.WriteKBps, b.ReadKBps+b.WriteKBps
			if av == bv {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
			return av < bv
		case 5:
			if a.OtherKBps == b.OtherKBps {
				return strings.ToLower(a.Name) < strings.ToLower(b.Name)
			}
			return a.OtherKBps < b.OtherKBps
		default:
			if strings.EqualFold(a.Name, b.Name) {
				return a.PID < b.PID
			}
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		v := less(out[i], out[j])
		if app.resourceProcSortDesc {
			return less(out[j], out[i])
		}
		return v
	})
	return out
}

func drawAdvancedResourceMonitor(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Продвинутый монитор", int(body.Left)+18, int(body.Top)+10, 300, 28, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	for i, r := range app.resourceAdvancedTabRects {
		label := []string{"Процессы", "Датчики"}[i]
		drawSelectableButton(hdc, r, label, app.resourceAdvancedView == i)
	}
	if app.resourceAdvancedView == 1 {
		drawAdvancedSensorMonitor(hdc, body)
		return
	}
	roundFill(hdc, app.resourceProcessSearchRect, surfaceButtonColor(), 9)

	sub := "Несистемные процессы"
	if app.settings.ShowSystemProcesses {
		sub = "Все процессы · системные помечены отдельно"
	}
	drawText(hdc, sub, int(body.Right)-360, int(body.Top)+16, 340, 18, 10, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	labels := []string{"Имя", "CPU", "GPU", "RAM", "Диск", "Сеть"}
	for i, r := range app.resourceProcSortRects {
		label := labels[i]
		if app.resourceProcSort == i {
			if app.resourceProcSortDesc {
				label += " ↓"
			} else {
				label += " ↑"
			}
		}
		drawSelectableButton(hdc, r, label, app.resourceProcSort == i)
	}

	list := filteredResourceProcesses()
	first := int(app.resourceProcScrollPx) / 43
	if ui2d.active {
		d2dPushClip(app.resourceProcListClip)
	}
	for slot, r := range app.resourceProcRows {
		idx := first + slot
		if idx < 0 || idx >= len(list) || r.Bottom <= app.resourceProcListClip.Top || r.Top >= app.resourceProcListClip.Bottom {
			continue
		}
		p := list[idx]
		c := surfaceButtonColor()
		if p.System {
			c = blendColor(c, theme.danger, .08)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .06*hv)
		}
		roundFill(hdc, rv, c, 10)
		cols := app.resourceProcSortRects
		name := p.Name
		if p.System {
			name += "  [SYSTEM]"
		}
		drawText(hdc, name, int(cols[0].Left)+10, int(r.Top), int(cols[0].Right-cols[0].Left)-16, int(r.Bottom-r.Top), 10, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, processPercentText(p.CPUPercent), int(cols[1].Left), int(r.Top), int(cols[1].Right-cols[1].Left)-4, int(r.Bottom-r.Top), 9, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, processPercentText(p.GPUPercent), int(cols[2].Left), int(r.Top), int(cols[2].Right-cols[2].Left)-4, int(r.Bottom-r.Top), 9, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, formatRAMMB(p.RAMMB), int(cols[3].Left), int(r.Top), int(cols[3].Right-cols[3].Left)-4, int(r.Bottom-r.Top), 9, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, formatRateValueKB(p.ReadKBps+p.WriteKBps), int(cols[4].Left), int(r.Top), int(cols[4].Right-cols[4].Left)-4, int(r.Bottom-r.Top), 9, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, formatNetworkRateKB(p.OtherKBps), int(cols[5].Left), int(r.Top), int(cols[5].Right-cols[5].Left)-4, int(r.Bottom-r.Top), 9, 600, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if ui2d.active {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.resourceProcScrollTrack, app.resourceProcScrollThumb)
	if len(list) == 0 {
		drawText(hdc, "Нет процессов, подходящих под текущий фильтр.", int(app.resourceProcListClip.Left)+12, int(app.resourceProcListClip.Top)+22, int(app.resourceProcListClip.Right-app.resourceProcListClip.Left)-24, 44, 11, 400, theme.muted, DT_LEFT|DT_VCENTER)
	}
}

func hardwareSensorUnit(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "temperature":
		return "°C"
	case "voltage":
		return "V"
	case "current":
		return "A"
	case "power":
		return "W"
	case "clock":
		return "MHz"
	case "load", "control", "level":
		return "%"
	case "fan":
		return "RPM"
	case "flow":
		return "L/h"
	case "frequency":
		return "Hz"
	case "data":
		return "GB"
	case "smalldata":
		return "MB"
	case "throughput":
		return "B/s"
	case "energy":
		return "mWh"
	case "timespan":
		return "s"
	case "timing":
		return "ns"
	case "noise":
		return "dBA"
	case "conductivity":
		return "µS/cm"
	case "humidity":
		return "%"
	case "factor":
		return "×"
	default:
		return ""
	}
}

func hardwareSensorValueText(s HardwareSensor, which int) string {
	v := s.Value
	has := true
	if which == 1 {
		v, has = s.Min, s.HasMin
	} else if which == 2 {
		v, has = s.Max, s.HasMax
	}
	if !has || math.IsNaN(v) || math.IsInf(v, 0) {
		return "—"
	}
	unit := hardwareSensorUnit(s.SensorType)
	abs := math.Abs(v)
	dec := 1
	if strings.EqualFold(s.SensorType, "Voltage") || strings.EqualFold(s.SensorType, "Current") || strings.EqualFold(s.SensorType, "Factor") {
		dec = 3
	} else if abs >= 1000 || strings.EqualFold(s.SensorType, "Fan") || strings.EqualFold(s.SensorType, "Clock") {
		dec = 0
	} else if abs < 10 && unit == "" {
		dec = 2
	}
	return fmt.Sprintf("%.*f%s", dec, v, unit)
}

func drawAdvancedSensorMonitor(hdc uintptr, body RECT) {
	if !temperatureDisplayEnabled() {
		drawText(hdc, "Аппаратные датчики отключены", int(body.Right)-280, int(body.Top)+16, 260, 18, 9, 600, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, "Установите аппаратные датчики в Настройки → Данные. До установки PowerPilot не публикует низкоуровневые показатели компонентов.", int(app.resourceProcListClip.Left)+12, int(app.resourceProcListClip.Top)+28, int(app.resourceProcListClip.Right-app.resourceProcListClip.Left)-24, 70, 11, 450, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK)
		return
	}
	for i, r := range app.resourceSensorTypeRects {
		if i < len(resourceSensorCategories) {
			drawSelectableButton(hdc, r, resourceSensorCategories[i].Label, app.resourceSensorView == i)
		}
	}
	sensors := filteredHardwareSensors()
	rows := hardwareSensorDisplayRows()
	all := hardwareSensorsSnapshot()
	cat := resourceSensorCategories[clampInt(app.resourceSensorView, 0, len(resourceSensorCategories)-1)].Label
	status := fmt.Sprintf("Аппаратных показателей: %d · %s: %d", len(all), cat, len(sensors))
	if at := hardwareSensorsLastUpdated(); !at.IsZero() {
		status += " · обновлено " + at.Format("15:04:05")
	}
	drawText(hdc, status, int(body.Right)-420, int(body.Top)+16, 400, 18, 9, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	left := int(app.resourceProcListClip.Left)
	right := int(app.resourceProcListClip.Right)
	width := max(1, right-left)
	deviceW := width * 31 / 100
	sensorW := width * 31 / 100
	valueW := (width - deviceW - sensorW) / 3
	headerY := int(app.resourceProcListClip.Top) - 27
	headers := []struct {
		x, w int
		t    string
	}{
		{left, deviceW, "Компонент / устройство"},
		{left + deviceW, sensorW, "Датчик"},
		{left + deviceW + sensorW, valueW, "Сейчас"},
		{left + deviceW + sensorW + valueW, valueW, "Мин."},
		{left + deviceW + sensorW + valueW*2, right - (left + deviceW + sensorW + valueW*2), "Макс."},
	}
	for _, h := range headers {
		drawText(hdc, h.t, h.x+6, headerY, h.w-12, 20, 9, 650, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}

	first := int(app.resourceProcScrollPx) / 43
	if ui2d.active {
		d2dPushClip(app.resourceProcListClip)
	}
	for slot, r := range app.resourceProcRows {
		idx := first + slot
		if idx < 0 || idx >= len(rows) || r.Bottom <= app.resourceProcListClip.Top || r.Top >= app.resourceProcListClip.Bottom {
			continue
		}
		row := rows[idx]
		c := surfaceButtonColor()
		if row.IsGroup {
			c = blendColor(c, theme.accent2, .065)
		}
		rv, hv := hoverCardRect(r)
		if hv > 0 {
			c = blendColor(c, theme.accent2, .07*hv)
		}
		roundFill(hdc, rv, c, 10)
		y := int(r.Top)
		h := int(r.Bottom - r.Top)
		if row.IsGroup {
			arrow := "▶"
			if row.Expanded {
				arrow = "▼"
			}
			indent := row.Level * 22
			drawText(hdc, arrow, left+8+indent, y, 18, h, 10, 700, theme.accent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
			label := row.Label
			if label == "" {
				label = row.Hardware
			}
			labelColor := theme.text
			weight := 650
			if row.Level > 0 {
				labelColor = theme.accent2
				weight = 600
			}
			drawText(hdc, label, left+30+indent, y, deviceW+sensorW-42-indent, h, 10, weight, labelColor, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			countText := fmt.Sprintf("Датчиков: %d", row.Count)
			drawText(hdc, countText, left+deviceW+sensorW, y, right-(left+deviceW+sensorW)-10, h, 9, 500, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
			continue
		}
		sensor := row.Sensor
		indent := row.Level * 18
		drawText(hdc, "↳", left+12+indent, y, 18, h, 9, 600, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
		componentText := sensor.Hardware
		if row.Level >= 2 {
			componentText = hardwareSensorTypeLabel(sensor.SensorType)
		}
		drawText(hdc, componentText, left+34+indent, y, deviceW-40-indent, h, 8, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, sensor.Name, left+deviceW+8, y, sensorW-14, h, 9, 500, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, hardwareSensorValueText(sensor, 0), left+deviceW+sensorW, y, valueW-4, h, 9, 700, theme.accent2, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, hardwareSensorValueText(sensor, 1), left+deviceW+sensorW+valueW, y, valueW-4, h, 9, 550, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, hardwareSensorValueText(sensor, 2), left+deviceW+sensorW+valueW*2, y, right-(left+deviceW+sensorW+valueW*2)-4, h, 9, 550, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	if ui2d.active {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.resourceProcScrollTrack, app.resourceProcScrollThumb)
	if len(sensors) == 0 {
		drawText(hdc, "В этой категории провайдер пока не опубликовал показателей. Если HWMonitor видит их, а здесь пусто — это уже разница аппаратного провайдера, а не фильтра интерфейса PowerPilot.", int(app.resourceProcListClip.Left)+12, int(app.resourceProcListClip.Top)+18, int(app.resourceProcListClip.Right-app.resourceProcListClip.Left)-24, 64, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_WORDBREAK)
	}
}

func processPercentText(v float64) string {
	if v < 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", v)
}

func formatRAMMB(v float64) string {
	if v < 0 {
		return "—"
	}
	if v >= 1024 {
		return fmt.Sprintf("%.1f ГБ", v/1024)
	}
	return fmt.Sprintf("%.0f МБ", v)
}

func resourceStatsPeriodDuration() time.Duration {
	switch app.resourceStatsPeriod {
	case 0:
		return time.Hour
	case 1:
		return 6 * time.Hour
	case 2:
		return 24 * time.Hour
	case 3:
		return 7 * 24 * time.Hour
	case 4:
		return 30 * 24 * time.Hour
	case 5:
		return 90 * 24 * time.Hour
	case 6:
		return 365 * 24 * time.Hour
	default:
		return 0 // all time; only valid for traffic/activity archive views
	}
}

type resourceStatsSummaryData struct {
	count                                       int
	start, timeEnd                              time.Time
	cpu, gpu, ram, disk, network                float64
	cpuMax, gpuMax, ramMax, diskMax, networkMax float64
	appTrafficKB                                float64
	apps                                        []AppResourceStat
}

func buildResourceStatsSummary(samples []ResourceStatSample) resourceStatsSummaryData {
	var o resourceStatsSummaryData
	if len(samples) == 0 {
		return o
	}
	o.count = len(samples)
	o.start = samples[0].At
	o.timeEnd = samples[len(samples)-1].At
	appAgg := map[string]*struct {
		AppResourceStat
		N int
	}{}
	for _, s := range samples {
		o.cpu += s.CPU
		o.gpu += s.GPU
		o.ram += s.RAM
		o.disk += s.Disk
		o.network += s.NetworkKBps
		o.cpuMax = math.Max(o.cpuMax, s.CPU)
		o.gpuMax = math.Max(o.gpuMax, s.GPU)
		o.ramMax = math.Max(o.ramMax, s.RAM)
		o.diskMax = math.Max(o.diskMax, s.Disk)
		o.networkMax = math.Max(o.networkMax, s.NetworkKBps)
		if s.AppTrafficKB > 0 {
			o.appTrafficKB += s.AppTrafficKB
		} else {
			for _, a := range s.Apps {
				o.appTrafficKB += a.TrafficKB
			}
		}
		for _, a := range s.Apps {
			key := strings.ToLower(a.Name)
			ag := appAgg[key]
			if ag == nil {
				ag = &struct {
					AppResourceStat
					N int
				}{AppResourceStat: AppResourceStat{Name: a.Name}}
				appAgg[key] = ag
			}
			ag.CPU += a.CPU
			ag.GPU += a.GPU
			ag.RAMMB += a.RAMMB
			ag.ReadKBps += a.ReadKBps
			ag.WriteKBps += a.WriteKBps
			ag.NetworkKBps += a.NetworkKBps
			ag.TrafficKB += a.TrafficKB
			ag.ActiveSeconds += a.ActiveSeconds
			ag.N++
		}
	}
	n := float64(len(samples))
	o.cpu /= n
	o.gpu /= n
	o.ram /= n
	o.disk /= n
	o.network /= n
	for _, ag := range appAgg {
		if ag.N > 0 {
			d := float64(ag.N)
			ag.CPU /= d
			ag.GPU /= d
			ag.RAMMB /= d
			ag.ReadKBps /= d
			ag.WriteKBps /= d
			ag.NetworkKBps /= d
		}
		o.apps = append(o.apps, ag.AppResourceStat)
	}
	sort.Slice(o.apps, func(i, j int) bool {
		return o.apps[i].CPU+o.apps[i].GPU+o.apps[i].RAMMB/512+o.apps[i].NetworkKBps/256 > o.apps[j].CPU+o.apps[j].GPU+o.apps[j].RAMMB/512+o.apps[j].NetworkKBps/256
	})
	if len(o.apps) > 12 {
		o.apps = o.apps[:12]
	}
	return o
}

func formatTrafficKB(v float64) string {
	if v < 1024 {
		return fmt.Sprintf("%.0f КБ", v)
	}
	if v < 1024*1024 {
		return fmt.Sprintf("%.1f МБ", v/1024)
	}
	return fmt.Sprintf("%.2f ГБ", v/(1024*1024))
}
func formatActivitySeconds(v float64) string {
	sec := int(v + 0.5)
	if sec < 60 {
		return fmt.Sprintf("%dс", sec)
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dч %02dм", h, m)
	}
	return fmt.Sprintf("%dм", m)
}

func sortResourceStatsApps(apps []AppResourceStat, view, key int, desc bool) {
	less := func(a, b AppResourceStat) bool {
		if key == 0 {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		av, bv := 0.0, 0.0
		switch view {
		case 2:
			valuesA := []float64{0, a.CPU, a.GPU, a.RAMMB, a.ReadKBps + a.WriteKBps, a.NetworkKBps}
			valuesB := []float64{0, b.CPU, b.GPU, b.RAMMB, b.ReadKBps + b.WriteKBps, b.NetworkKBps}
			av, bv = valuesA[clampInt(key, 0, 5)], valuesB[clampInt(key, 0, 5)]
		case 3:
			av, bv = a.TrafficKB, b.TrafficKB
		case 4:
			av, bv = a.ActiveSeconds, b.ActiveSeconds
		}
		if av == bv {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return av < bv
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if desc {
			return less(apps[j], apps[i])
		}
		return less(apps[i], apps[j])
	})
}

func statsSortLabel(label string, key int) string {
	if app.resourceStatsSort != key {
		return label
	}
	if app.resourceStatsSortDesc {
		return label + " ↓"
	}
	return label + " ↑"
}

func prepareResourceStatsList(body RECT, listTop, count, stride int) int {
	viewBottom := int(body.Bottom) - 10
	viewH := max(1, viewBottom-listTop)
	contentH := max(0, count*stride)
	app.resourceStatsListScrollMax = float64(max(0, contentH-viewH))
	app.resourceStatsListScrollPx = clampFloat(app.resourceStatsListScrollPx, 0, app.resourceStatsListScrollMax)
	app.resourceStatsListScrollTarget = clampFloat(app.resourceStatsListScrollTarget, 0, app.resourceStatsListScrollMax)
	app.resourceStatsListScrollTrack = RECT{body.Right - 9, int32(listTop), body.Right - 4, int32(viewBottom)}
	app.resourceStatsListScrollThumb = scrollThumbRectPixels(app.resourceStatsListScrollTrack, contentH, viewH, app.resourceStatsListScrollPx)
	return listTop - int(app.resourceStatsListScrollPx)
}

func drawResourceStatistics(hdc uintptr, body RECT, w int) {
	drawText(hdc, "Статистика ресурсов", int(body.Left)+18, int(body.Top)+12, 300, 30, 20, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	periodLabels := []string{"1ч", "6ч", "24ч", "7д", "30д", "90д", "1 год", "Всё время"}
	for i, r := range app.resourceStatsPeriodRects {
		if r.Right > r.Left {
			drawSelectableButton(hdc, r, periodLabels[i], app.resourceStatsPeriod == i)
		}
	}
	viewLabels := []string{"Обзор", "По времени", "Приложения", "Трафик", "Активность"}
	for i, r := range app.resourceStatsViewRects {
		drawSelectableButton(hdc, r, viewLabels[i], app.resourceStatsView == i)
	}
	dur := resourceStatsPeriodDuration()
	if dur == 0 && app.resourceStatsView < 3 {
		dur = 30 * 24 * time.Hour
	}
	samples := resourceStatsForPeriod(dur)
	sum := buildResourceStatsSummary(samples)
	longTerm := false
	if (app.resourceStatsView == 3 || app.resourceStatsView == 4) && app.resourceStatsPeriod >= 3 {
		apps, traffic, start, end := resourceAppHistoryForPeriod(resourceStatsPeriodDuration())
		sum.apps = apps
		sum.appTrafficKB = traffic
		sum.start, sum.timeEnd = start, end
		sum.count = len(apps)
		longTerm = true
	}
	y := int(app.resourceStatsViewRects[0].Bottom) + 12
	if (app.resourceStatsView == 0 || app.resourceStatsView == 1) && app.resourceStatsGraphRects[0].Right > app.resourceStatsGraphRects[0].Left {
		graphLabels := []string{"Общий", "CPU", "GPU", "RAM", "Диск", "Сеть"}
		for i, r := range app.resourceStatsGraphRects {
			drawSelectableButton(hdc, r, graphLabels[i], app.resourceStatsGraphMode == i)
		}
		y = int(app.resourceStatsGraphRects[0].Bottom) + 10
	}
	coverage := "Пока нет накопленной статистики"
	if sum.count > 0 {
		if longTerm {
			span := "за выбранный период"
			if app.resourceStatsPeriod == 7 {
				span = "за всё время"
			}
			coverage = fmt.Sprintf("Накопительные данные %s · %s — %s · без ограничения срока хранения", span, sum.start.Format("02.01.2006"), sum.timeEnd.Format("02.01.2006"))
		} else {
			coverage = fmt.Sprintf("Собрано %d точек · %s — %s · детальная история хранится до 30 дней", sum.count, sum.start.Format("02.01 15:04"), sum.timeEnd.Format("02.01 15:04"))
		}
	}
	drawText(hdc, coverage, int(body.Left)+18, y, int(body.Right-body.Left)-36, 20, 10, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	y += 30

	if app.resourceStatsView == 1 {
		drawText(hdc, "Средняя активность по часу суток", int(body.Left)+18, y, 360, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		y += 26
		r := RECT{body.Left + 18, int32(y), body.Right - 18, int32(min(int(body.Bottom)-20, y+260))}
		drawStatsByHourGraph(hdc, r, samples, app.resourceStatsGraphMode)
		return
	}
	if app.resourceStatsView >= 2 && app.resourceStatsView <= 4 {
		if len(sum.apps) == 0 {
			drawText(hdc, "Данные приложений появятся после накопления статистики.", int(body.Left)+18, y+12, int(body.Right-body.Left)-36, 40, 11, 400, theme.muted, DT_LEFT|DT_VCENTER)
			return
		}
		apps := append([]AppResourceStat(nil), sum.apps...)
		sortResourceStatsApps(apps, app.resourceStatsView, app.resourceStatsSort, app.resourceStatsSortDesc)
		for i := range app.resourceStatsSortRects {
			app.resourceStatsSortRects[i] = RECT{}
		}
		left := int(body.Left) + 18
		right := int(body.Right) - 18
		if app.resourceStatsView == 2 {
			drawText(hdc, "Среднее потребление приложениями", left, y, 360, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			y += 28
			totalW := right - left
			nameW := max(155, totalW*32/100)
			rest := max(1, totalW-nameW)
			cw := rest / 5
			headers := []string{"Приложение", "CPU", "GPU", "RAM", "Диск", "Сеть"}
			xs := make([]int, 6)
			ws := make([]int, 6)
			xs[0], ws[0] = left, nameW
			for i := 1; i < 6; i++ {
				xs[i] = left + nameW + (i-1)*cw
				ws[i] = cw
				if i == 5 {
					ws[i] = right - xs[i]
				}
			}
			for i, h := range headers {
				app.resourceStatsSortRects[i] = RECT{int32(xs[i]), int32(y - 2), int32(xs[i] + ws[i]), int32(y + 22)}
				drawCompactSortButton(hdc, app.resourceStatsSortRects[i], statsSortLabel(h, i), app.resourceStatsSort == i)
			}
			y += 24
			listTop := y
			y = prepareResourceStatsList(body, y, len(apps), 39)
			listBottom := int(body.Bottom) - 10
			if ui2d.active {
				d2dPushClip(RECT{int32(left), int32(listTop), int32(right - 10), int32(listBottom)})
			}
			for _, a := range apps {
				if y+34 > listTop && y < listBottom {
					r := RECT{int32(left), int32(y), int32(right - 10), int32(y + 34)}
					roundFill(hdc, r, surfaceButtonColor(), 8)
					disk := a.ReadKBps + a.WriteKBps
					vals := []string{a.Name, fmt.Sprintf("%.1f%%", a.CPU), fmt.Sprintf("%.1f%%", a.GPU), formatRAMMB(a.RAMMB), formatRateValueKB(disk), formatNetworkRateKB(a.NetworkKBps)}
					for i, v := range vals {
						flags := uint32(DT_CENTER | DT_VCENTER | DT_SINGLELINE | DT_END_ELLIPSIS)
						col := theme.muted
						if i == 0 {
							flags, col = DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS, theme.text
						}
						drawText(hdc, v, xs[i]+6, y, ws[i]-12, 34, 8, 500, col, flags)
					}
				}
				y += 39
			}
			if ui2d.active {
				d2dPopClip()
			}
			drawScrollBar(hdc, app.resourceStatsListScrollTrack, app.resourceStatsListScrollThumb)
			return
		}
		if app.resourceStatsView == 3 {
			drawText(hdc, "Потраченный интернет-трафик приложениями", left, y, right-left, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			drawText(hdc, "Всего за период: "+formatTrafficKB(sum.appTrafficKB), left, y+21, right-left, 18, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
			y += 50
			nameW := max(220, (right-left)*56/100)
			app.resourceStatsSortRects[0] = RECT{int32(left), int32(y - 2), int32(left + nameW), int32(y + 22)}
			app.resourceStatsSortRects[1] = RECT{int32(left + nameW), int32(y - 2), int32(right), int32(y + 22)}
			drawCompactSortButton(hdc, app.resourceStatsSortRects[0], statsSortLabel("Приложение", 0), app.resourceStatsSort == 0)
			drawCompactSortButton(hdc, app.resourceStatsSortRects[1], statsSortLabel("Потрачено", 1), app.resourceStatsSort == 1)
			y += 24
			listTop := y
			y = prepareResourceStatsList(body, y, len(apps), 39)
			listBottom := int(body.Bottom) - 10
			if ui2d.active {
				d2dPushClip(RECT{int32(left), int32(listTop), int32(right - 10), int32(listBottom)})
			}
			for _, a := range apps {
				if y+34 > listTop && y < listBottom {
					r := RECT{int32(left), int32(y), int32(right - 10), int32(y + 34)}
					roundFill(hdc, r, surfaceButtonColor(), 8)
					drawText(hdc, a.Name, left+10, y, nameW-16, 34, 9, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
					drawText(hdc, formatTrafficKB(a.TrafficKB), left+nameW+8, y, right-left-nameW-28, 34, 9, 550, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
				}
				y += 39
			}
			if ui2d.active {
				d2dPopClip()
			}
			drawScrollBar(hdc, app.resourceStatsListScrollTrack, app.resourceStatsListScrollThumb)
			return
		}
		drawText(hdc, "Активность приложений", left, y, right-left, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, "Приблизительное время, когда приложение было активным окном.", left, y+21, right-left, 18, 9, 500, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		y += 50
		nameW := max(220, (right-left)*56/100)
		app.resourceStatsSortRects[0] = RECT{int32(left), int32(y - 2), int32(left + nameW), int32(y + 22)}
		app.resourceStatsSortRects[1] = RECT{int32(left + nameW), int32(y - 2), int32(right), int32(y + 22)}
		drawCompactSortButton(hdc, app.resourceStatsSortRects[0], statsSortLabel("Приложение", 0), app.resourceStatsSort == 0)
		drawCompactSortButton(hdc, app.resourceStatsSortRects[1], statsSortLabel("Активность", 1), app.resourceStatsSort == 1)
		y += 24
		listTop := y
		y = prepareResourceStatsList(body, y, len(apps), 39)
		listBottom := int(body.Bottom) - 10
		if ui2d.active {
			d2dPushClip(RECT{int32(left), int32(listTop), int32(right - 10), int32(listBottom)})
		}
		for _, a := range apps {
			if y+34 > listTop && y < listBottom {
				r := RECT{int32(left), int32(y), int32(right - 10), int32(y + 34)}
				roundFill(hdc, r, surfaceButtonColor(), 8)
				drawText(hdc, a.Name, left+10, y, nameW-16, 34, 9, 550, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
				drawText(hdc, formatActivitySeconds(a.ActiveSeconds), left+nameW+8, y, right-left-nameW-28, 34, 9, 550, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
			}
			y += 39
		}
		if ui2d.active {
			d2dPopClip()
		}
		drawScrollBar(hdc, app.resourceStatsListScrollTrack, app.resourceStatsListScrollThumb)
		return
	}

	vals := []struct {
		name     string
		avg, max float64
		net      bool
	}{{"CPU", sum.cpu, sum.cpuMax, false}, {"GPU", sum.gpu, sum.gpuMax, false}, {"RAM", sum.ram, sum.ramMax, false}, {"Диск", sum.disk, sum.diskMax, false}, {"Сеть", sum.network, sum.networkMax, true}}
	gap := 8
	cardW := (int(body.Right-body.Left) - 36 - gap*4) / 5
	for i, v := range vals {
		x := int(body.Left) + 18 + i*(cardW+gap)
		r := RECT{int32(x), int32(y), int32(x + cardW), int32(y + 66)}
		roundFill(hdc, r, surfaceButtonColor(), 11)
		drawText(hdc, v.name, x+10, y+6, cardW-20, 16, 9, 600, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		avg := fmt.Sprintf("%.1f%%", v.avg)
		mx := fmt.Sprintf("макс %.0f%%", v.max)
		if v.net {
			avg = formatNetworkRateKB(v.avg)
			mx = "макс " + formatNetworkRateKB(v.max)
		}
		drawText(hdc, avg, x+10, y+22, cardW-20, 22, 15, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, mx, x+10, y+45, cardW-20, 14, 8, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
	y += 82
	graphR := RECT{body.Left + 18, int32(y), body.Right - 18, int32(min(int(body.Bottom)-18, y+175))}
	drawStatsHistoryGraph(hdc, graphR, samples, app.resourceStatsGraphMode)
}

func drawStatsByHourGraph(hdc uintptr, r RECT, samples []ResourceStatSample, mode int) {
	roundFill(hdc, r, blendColor(surfacePanelColor(), surfaceButtonColor(), .42), 12)
	if len(samples) == 0 {
		drawText(hdc, "Для этой выборки пока нет данных.", int(r.Left)+12, int(r.Top), int(r.Right-r.Left)-24, int(r.Bottom-r.Top), 10, 400, theme.muted, DT_CENTER|DT_VCENTER)
		return
	}
	type bucket struct {
		cpu, gpu, ram, disk, network float64
		n                            int
	}
	var b [24]bucket
	for _, s := range samples {
		h := s.At.Local().Hour()
		b[h].cpu += s.CPU
		b[h].gpu += s.GPU
		b[h].ram += s.RAM
		b[h].disk += s.Disk
		b[h].network += s.NetworkKBps
		b[h].n++
	}
	axisLabelW := int32(42)
	if mode == 5 {
		axisLabelW = 64
	}
	plot := RECT{r.Left + axisLabelW + 8, r.Top + 26, r.Right - 12, r.Bottom - 28}
	yMax := 100.0
	if mode == 5 {
		yMax = 1
		for _, bucket := range b {
			if bucket.n > 0 {
				yMax = math.Max(yMax, bucket.network/float64(bucket.n))
			}
		}
	}
	for k := 0; k <= 4; k++ {
		yy := float32(plot.Top) + float32(plot.Bottom-plot.Top)*float32(k)/4
		d2dDrawLine(float32(plot.Left), yy, float32(plot.Right), yy, .45, blendColor(theme.border, theme.muted, .18))
		value := yMax * (1 - float64(k)/4)
		label := fmt.Sprintf("%.0f%%", value)
		if mode == 5 {
			label = formatNetworkRateKB(value)
		}
		drawText(hdc, label, int(r.Left)+4, int(yy)-8, int(axisLabelW), 16, 8, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
	cols := []uint32{theme.accent, rgb(235, 145, 72), theme.success, theme.accent2, theme.accent}
	seriesIndices := []int{0, 1, 2, 3}
	if mode > 0 {
		seriesIndices = []int{mode - 1}
	}
	for _, si := range seriesIndices {
		last := false
		var lx, ly float32
		for h := 0; h < 24; h++ {
			if b[h].n == 0 {
				last = false
				continue
			}
			vals := []float64{b[h].cpu / float64(b[h].n), b[h].gpu / float64(b[h].n), b[h].ram / float64(b[h].n), b[h].disk / float64(b[h].n), b[h].network / float64(b[h].n)}
			x := float32(plot.Left) + float32(plot.Right-plot.Left)*float32(h)/23
			yy := float32(plot.Bottom) - float32(plot.Bottom-plot.Top)*float32(clampFloat(vals[si], 0, yMax)/yMax)
			if last {
				d2dDrawLine(lx, ly, x, yy, 1.5, cols[si])
			}
			lx, ly = x, yy
			last = true
		}
	}
	for h := 0; h < 24; h += 3 {
		x := int(plot.Left) + int(float32(plot.Right-plot.Left)*float32(h)/23)
		drawText(hdc, fmt.Sprintf("%02d", h), x-12, int(plot.Bottom)+5, 24, 16, 8, 400, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	legend := []struct {
		n string
		c uint32
	}{{"CPU", cols[0]}, {"GPU", cols[1]}, {"RAM", cols[2]}, {"Диск", cols[3]}, {"Сеть", cols[4]}}
	lx := int(r.Left) + 12
	for i, v := range legend {
		if mode == 0 && i == 4 {
			continue
		}
		if mode > 0 && i != mode-1 {
			continue
		}
		drawText(hdc, v.n, lx, int(r.Top)+5, 45, 16, 8, 600, v.c, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		lx += 46
	}
}

func formatGraphTimeTick(t time.Time, span time.Duration) string {
	if span >= 48*time.Hour {
		return t.Format("02.01 15:04")
	}
	if span >= time.Hour {
		return t.Format("15:04")
	}
	return t.Format("15:04:05")
}

func formatRelativeGraphTick(remaining time.Duration) string {
	if remaining <= time.Second {
		return "0"
	}
	remaining = remaining.Round(time.Second)
	if remaining >= time.Hour {
		h := int(remaining / time.Hour)
		m := int((remaining % time.Hour) / time.Minute)
		if m > 0 {
			return fmt.Sprintf("−%dч %dм", h, m)
		}
		return fmt.Sprintf("−%dч", h)
	}
	if remaining >= time.Minute {
		m := int(remaining / time.Minute)
		s := int((remaining % time.Minute) / time.Second)
		if s > 0 {
			return fmt.Sprintf("−%dм %dс", m, s)
		}
		return fmt.Sprintf("−%dм", m)
	}
	return fmt.Sprintf("−%dс", int(remaining/time.Second))
}

func resourceGraphTimeTick(t, end time.Time, span time.Duration) string {
	if app.settings.ResourceTimelineMode == 1 {
		return formatRelativeGraphTick(end.Sub(t))
	}
	return formatGraphTimeTick(t, span)
}

func drawStatsHistoryGraph(hdc uintptr, r RECT, samples []ResourceStatSample, mode int) {
	roundFill(hdc, r, blendColor(surfacePanelColor(), surfaceButtonColor(), .42), 12)
	if len(samples) < 2 {
		drawText(hdc, "История появится после накопления измерений.", int(r.Left)+12, int(r.Top), int(r.Right-r.Left)-24, int(r.Bottom-r.Top), 10, 400, theme.muted, DT_CENTER|DT_VCENTER)
		return
	}
	axisLabelW := int32(42)
	if mode == 5 {
		axisLabelW = 64
	}
	plot := RECT{r.Left + axisLabelW + 8, r.Top + 20, r.Right - 10, r.Bottom - 30}
	for k := 1; k <= 3; k++ {
		yy := float32(plot.Top) + float32(plot.Bottom-plot.Top)*float32(k)/4
		d2dDrawLine(float32(plot.Left), yy, float32(plot.Right), yy, .45, blendColor(theme.border, theme.muted, .18))
	}
	gpuColor := rgb(235, 145, 72)
	type ser struct {
		name  string
		color uint32
		vals  []float64
		max   float64
	}
	mk := func(name string, c uint32, maxv float64, f func(ResourceStatSample) float64) ser {
		v := make([]float64, len(samples))
		for i, s := range samples {
			v[i] = f(s)
		}
		return ser{name, c, v, maxv}
	}
	series := []ser{}
	switch mode {
	case 1:
		series = []ser{mk("CPU", theme.accent, 100, func(s ResourceStatSample) float64 { return s.CPU })}
	case 2:
		series = []ser{mk("GPU", gpuColor, 100, func(s ResourceStatSample) float64 { return s.GPU })}
	case 3:
		series = []ser{mk("RAM", theme.success, 100, func(s ResourceStatSample) float64 { return s.RAM })}
	case 4:
		series = []ser{mk("Диск", theme.accent2, 100, func(s ResourceStatSample) float64 { return s.Disk })}
	case 5:
		mx := 1.0
		for _, s := range samples {
			if s.NetworkKBps > mx {
				mx = s.NetworkKBps
			}
		}
		series = []ser{mk("Сеть", theme.accent, mx, func(s ResourceStatSample) float64 { return s.NetworkKBps })}
	default:
		series = []ser{mk("CPU", theme.accent, 100, func(s ResourceStatSample) float64 { return s.CPU }), mk("GPU", gpuColor, 100, func(s ResourceStatSample) float64 { return s.GPU }), mk("RAM", theme.success, 100, func(s ResourceStatSample) float64 { return s.RAM }), mk("Диск", theme.accent2, 100, func(s ResourceStatSample) float64 { return s.Disk })}
	}
	axisMax := 100.0
	if mode == 5 && len(series) > 0 {
		axisMax = series[0].max
	}
	for k := 0; k <= 4; k++ {
		yy := float32(plot.Top) + float32(plot.Bottom-plot.Top)*float32(k)/4
		value := axisMax * (1 - float64(k)/4)
		label := fmt.Sprintf("%.0f%%", value)
		if mode == 5 {
			label = formatNetworkRateKB(value)
		}
		drawText(hdc, label, int(r.Left)+4, int(yy)-8, int(axisLabelW), 16, 8, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
	startAt, endAt := samples[0].At, samples[len(samples)-1].At
	span := endAt.Sub(startAt)
	if span <= 0 {
		span = time.Second
	}
	xFor := func(t time.Time) float32 {
		f := t.Sub(startAt).Seconds() / span.Seconds()
		return float32(plot.Left) + float32(plot.Right-plot.Left)*float32(clampFloat(f, 0, 1))
	}
	for _, se := range series {
		for i := 1; i < len(se.vals); i++ {
			x1 := xFor(samples[i-1].At)
			x2 := xFor(samples[i].At)
			y1 := float32(plot.Bottom) - float32(plot.Bottom-plot.Top)*float32(clampFloat(se.vals[i-1], 0, se.max)/se.max)
			y2 := float32(plot.Bottom) - float32(plot.Bottom-plot.Top)*float32(clampFloat(se.vals[i], 0, se.max)/se.max)
			d2dDrawLine(x1, y1, x2, y2, 1.5, se.color)
		}
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
		x0, ww := x-45, 90
		if i == 0 {
			flags = DT_LEFT | DT_VCENTER | DT_SINGLELINE
			x0 = int(plot.Left)
			ww = 100
		}
		if i == tickCount-1 {
			flags = DT_RIGHT | DT_VCENTER | DT_SINGLELINE
			x0 = int(plot.Right) - 100
			ww = 100
		}
		drawText(hdc, resourceGraphTimeTick(tm, endAt, span), x0, int(plot.Bottom)+5, ww, 16, 8, 400, theme.muted, flags)
	}
	lx := int(r.Left) + 12
	for _, se := range series {
		drawText(hdc, se.name, lx, int(r.Top)+3, 52, 14, 8, 600, se.color, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		lx += 55
	}
	if mode == 5 {
		mx := series[0].max
		drawText(hdc, "шкала до "+formatNetworkRateKB(mx), int(r.Right)-170, int(r.Top)+3, 158, 14, 8, 400, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
	}
}
