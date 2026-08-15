//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	diagInfo = iota
	diagOK
	diagWait
	diagError
)

type DiagnosticLine struct {
	Level  int
	Title  string
	Detail string
}

func appendRunHistory(kind, detail, runID string) {
	if strings.TrimSpace(runID) != "" {
		detail = "run=" + runID + " | " + detail
	}
	appendHistory(kind, detail)
}

func newRunID() string { return fmt.Sprintf("run-%d", time.Now().UnixNano()) }

func historyRunID(detail string) string {
	if !strings.HasPrefix(detail, "run=") {
		return ""
	}
	if i := strings.Index(detail, " | "); i > 4 {
		return detail[4:i]
	}
	return ""
}

func historyDisplayDetail(detail string) string {
	if i := strings.Index(detail, " | "); strings.HasPrefix(detail, "run=") && i >= 0 {
		return detail[i+3:]
	}
	return detail
}

func diagnoseCondition(c AutomationCondition) (bool, string) {
	metric := metricsSnapshot()
	switch c.Type {
	case condCPU:
		ok := compareMetric(metric.CPU, c.Threshold, c.Compare)
		return ok, fmt.Sprintf("CPU %.1f%% · требуется %s %.0f%%", metric.CPU, compareSymbol(c.Compare), c.Threshold)
	case condGPU:
		if metric.GPU < 0 {
			return false, "GPU: метрика недоступна"
		}
		ok := compareMetric(metric.GPU, c.Threshold, c.Compare)
		return ok, fmt.Sprintf("GPU %.1f%% · требуется %s %.0f%%", metric.GPU, compareSymbol(c.Compare), c.Threshold)
	case condNetwork:
		ok := compareMetric(metric.NetworkKBps, c.Threshold, c.Compare)
		return ok, fmt.Sprintf("Сеть %.1f КБ/с · требуется %s %.0f КБ/с", metric.NetworkKBps, compareSymbol(c.Compare), c.Threshold)
	case condDisk:
		if metric.DiskPercent < 0 {
			return false, "Диск: метрика недоступна"
		}
		ok := compareMetric(metric.DiskPercent, c.Threshold, c.Compare)
		return ok, fmt.Sprintf("Диск %.1f%% · требуется %s %.0f%%", metric.DiskPercent, compareSymbol(c.Compare), c.Threshold)
	case condBatteryPercent:
		if metric.BatteryPercent < 0 {
			return false, "Батарея: метрика недоступна"
		}
		ok := compareMetric(metric.BatteryPercent, c.Threshold, c.Compare)
		return ok, fmt.Sprintf("Батарея %.0f%% · требуется %s %.0f%%", metric.BatteryPercent, compareSymbol(c.Compare), c.Threshold)
	case condACPower:
		wantAC := c.Compare > 0
		current := "от батареи"
		if metric.OnAC {
			current = "от сети"
		}
		ok := metric.OnAC == wantAC
		want := "от батареи"
		if wantAC {
			want = "от сети"
		}
		return ok, fmt.Sprintf("Сейчас питание %s · требуется %s", current, want)
	case condDiskFree:
		free, total, _, ok := diskFreeInfo(c.Text)
		if !ok {
			return false, "Свободное место: диск или путь недоступен"
		}
		passed := compareMetric(free, c.Threshold, c.Compare)
		return passed, fmt.Sprintf("Свободно %.1f из %.1f ГБ · требуется %s %.1f ГБ", free, total, compareSymbol(c.Compare), c.Threshold)
	case condFolderCount:
		path := strings.TrimSpace(c.Text)
		if path == "" {
			return false, "Папка не выбрана"
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return false, "Папка недоступна: " + shortenMiddle(path, 42)
		}
		files := 0
		for _, entry := range entries {
			if !entry.IsDir() {
				files++
			}
		}
		count := float64(files)
		passed := compareMetric(count, c.Threshold, c.Compare)
		return passed, fmt.Sprintf("Файлов %d · требуется %s %.0f", files, compareSymbol(c.Compare), c.Threshold)
	case condProcessExit:
		name := strings.TrimSpace(c.Text)
		if name == "" {
			return false, "Процесс не выбран"
		}
		running := processRunning(name)
		if running {
			return false, name + " ещё запущен"
		}
		return true, name + " завершён"
	case condWindowExists:
		q := strings.TrimSpace(c.Text)
		if q == "" {
			return false, "Окно/процесс не задан"
		}
		ok := windowMatch040(q, false, false)
		if ok {
			return true, "Окно найдено"
		}
		return false, "Окно пока не найдено"
	case condWindowMissing:
		q := strings.TrimSpace(c.Text)
		if q == "" {
			return false, "Окно/процесс не задан"
		}
		ok := !windowMatch040(q, false, false)
		if ok {
			return true, "Окно отсутствует"
		}
		return false, "Окно ещё открыто"
	case condWindowActive:
		q := strings.TrimSpace(c.Text)
		if q == "" {
			return false, "Окно/процесс не задан"
		}
		ok := windowMatch040(q, true, false)
		if ok {
			return true, "Нужное окно активно"
		}
		return false, "Активно другое окно"
	case condWindowTitle:
		q := strings.TrimSpace(c.Text)
		if q == "" {
			return false, "Фрагмент заголовка не задан"
		}
		ok := windowMatch040(q, false, true)
		if ok {
			return true, "Заголовок найден"
		}
		return false, "Подходящего заголовка нет"
	case condAudioSilent:
		peak := float32(-1)
		label := "системного звука"
		if strings.TrimSpace(c.Text) != "" {
			label = c.Text
			if p, found := audioPeakForProcess040(c.Text); found {
				peak = p
			} else {
				return false, "Аудиосессия процесса пока не найдена: " + c.Text
			}
		} else {
			peak = audioPeak040()
		}
		if peak < 0 {
			return false, "Метрика звука недоступна"
		}
		ok := peak < 0.008
		return ok, fmt.Sprintf("%s · пиковый уровень %.1f%%", label, peak*100)
	case condFileStable:
		path := strings.TrimSpace(c.Text)
		if path == "" {
			return false, "Файл или папка не выбраны"
		}
		if _, err := os.Stat(path); err != nil {
			return false, "Путь недоступен: " + filepath.Base(path)
		}
		conditionRuntimeMu.Lock()
		st := conditionRuntimes[c.ID]
		conditionRuntimeMu.Unlock()
		if st == nil || !st.fileInit {
			return false, "Стабильность файла ещё не измерялась"
		}
		held := time.Since(st.lastSizeChange)
		need := time.Duration(max(c.HoldSeconds, 5)) * time.Second
		return held >= need, fmt.Sprintf("Размер не меняется %s / %s", formatDuration(held), formatDuration(need))
	}
	return false, "Неизвестное условие"
}

func compareSymbol(v int) string {
	if v > 0 {
		return "≥"
	}
	return "≤"
}

func buildDiagnosticReport(dry bool) []DiagnosticLine {
	lines := []DiagnosticLine{}
	title := "Диагностика текущей задачи"
	if dry {
		title = "Тестовый прогон — без выполнения действий"
	}
	intro := "Показываю текущее состояние триггера, условий, защиты и шагов сценария."
	if dry {
		intro = "PowerPilot ничего не выключит, не закроет и не запустит во время тестового прогона."
	}
	lines = append(lines, DiagnosticLine{diagInfo, title, intro})

	if app.schedule.active && !dry {
		switch app.schedule.mode {
		case 0, 1:
			rem := time.Until(app.schedule.target)
			if rem < 0 {
				rem = 0
			}
			lvl := diagWait
			if rem == 0 {
				lvl = diagOK
			}
			lines = append(lines, DiagnosticLine{lvl, "Основной триггер", "Осталось: " + formatDuration(rem)})
		case 2:
			idle := getIdleDuration()
			lvl := diagWait
			if idle >= app.schedule.idleThreshold {
				lvl = diagOK
			}
			lines = append(lines, DiagnosticLine{lvl, "Простой", fmt.Sprintf("%s / %s", formatDuration(idle), formatDuration(app.schedule.idleThreshold))})
		case 3:
			running := processRunning(app.schedule.watchProcess)
			lvl := diagWait
			if !running {
				lvl = diagOK
			}
			detail := app.schedule.watchProcess + " ещё запущен"
			if !running {
				detail = app.schedule.watchProcess + " завершён"
			}
			lines = append(lines, DiagnosticLine{lvl, "Процесс", detail})
		case 4:
			lines = append(lines, DiagnosticLine{diagOK, "Расписание", "Задача уже активирована расписанием"})
		case 5:
			lines = append(lines, DiagnosticLine{diagInfo, "Триггер", "Запуск определяется только блоками условий"})
		}
	} else if dry {
		lines = append(lines, DiagnosticLine{diagInfo, "Основной триггер", currentScenarioWhenSummary() + " — ожидание пропущено в тестовом прогоне"})
	} else {
		lines = append(lines, DiagnosticLine{diagWait, "Основной триггер", "Задача сейчас не запущена · настройка: " + currentScenarioWhenSummary()})
	}

	conds := app.settings.AdvancedConditions
	if app.scenarioSavedDraft {
		conds = app.savedEditDraft.Conditions
	}
	if !app.scenarioSavedDraft && app.settings.TaskKind == 0 && !(app.schedule.active && !dry) {
		conds = nil
	}
	if app.schedule.active && !dry {
		conds = app.schedule.conditions
	}
	vals := make([]bool, 0, len(conds))
	enabled := make([]AutomationCondition, 0, len(conds))
	valuesByID := make(map[string]bool, len(conds))
	hasGroups := false
	for _, c := range conds {
		if !c.Enabled {
			continue
		}
		if c.Type == condGroup {
			hasGroups = true
			lines = append(lines, DiagnosticLine{diagInfo, "Составное условие", "Группа вычисляется по вложенным условиям"})
			continue
		}
		ok, detail := diagnoseCondition(c)
		lvl := diagWait
		if ok {
			lvl = diagOK
		}
		title := conditionSummary(c)
		if c.OpenGroups > 0 {
			title = strings.Repeat("(", c.OpenGroups) + title
		}
		if c.CloseGroups > 0 {
			title += strings.Repeat(")", c.CloseGroups)
		}
		lines = append(lines, DiagnosticLine{lvl, title, detail})
		vals = append(vals, ok)
		enabled = append(enabled, c)
		valuesByID[c.ID] = ok
	}
	if len(enabled) > 0 || hasGroups {
		ok := evalGroupedConditionValues(enabled, vals)
		if hasGroups {
			ok, _ = evalCompoundConditionValues(conds, valuesByID, "", map[string]bool{})
		}
		lvl := diagWait
		if ok {
			lvl = diagOK
		}
		lines = append(lines, DiagnosticLine{lvl, "Итог выражения условий", fmt.Sprintf("%v", ok)})
	}
	safe, why := checkSafetyRules()
	if safe {
		lines = append(lines, DiagnosticLine{diagOK, "Защитные правила", "Блокировок нет"})
	} else {
		lines = append(lines, DiagnosticLine{diagWait, "Защитные правила", why})
	}

	steps := app.settings.ActionSteps
	if app.scenarioSavedDraft {
		steps = app.savedEditDraft.Steps
	}
	if !app.scenarioSavedDraft && app.settings.TaskKind == 0 && !(app.schedule.active && !dry) {
		steps = nil
	}
	if app.schedule.active && !dry {
		steps = app.schedule.steps
	}
	for i, st := range steps {
		detail := stepSummary(st)
		lvl := diagInfo
		if st.OnError == 1 {
			detail += " · ошибка → остановить"
		}
		if st.OnError == 2 {
			detail += fmt.Sprintf(" · ошибка → до %d повторов", st.Retries)
		}
		if dry {
			switch st.Type {
			case stepCloseProcesses:
				detail = fmt.Sprintf("Будут закрыты процессы шага: %d", len(st.Processes))
			case stepWait:
				detail = fmt.Sprintf("Будет ожидание %d сек", max(st.Value, 1))
			case stepRunCommand:
				if strings.TrimSpace(st.Text) == "" {
					lvl = diagError
					detail = "Команда не задана"
				} else {
					detail = "Была бы запущена: " + shortenMiddle(st.Text, 58)
				}
			case stepNotify:
				detail = "Было бы показано уведомление: " + shortenMiddle(st.Text, 58)
			case stepMonitorOff:
				detail = "Монитор был бы выключен"
			case stepMonitorOn:
				detail = "Монитор был бы включён"
			case stepSetVolume:
				detail = fmt.Sprintf("Системная громкость была бы установлена на %d%%", clampInt(st.Value, 0, 100))
			case stepMute:
				if st.Value != 0 {
					detail = "Системный звук был бы выключен"
				} else {
					detail = "Системный звук был бы включён"
				}
			case stepLockWorkstation:
				detail = "Рабочая станция была бы заблокирована"
			case stepPowerPlan:
				names := []string{"Энергосбережение", "Сбалансированный", "Высокая производительность"}
				detail = "Был бы включён план питания: " + names[clampInt(st.Value, 0, 2)]
			}
			if st.DelayAfter > 0 {
				detail += fmt.Sprintf(" · затем пауза %d сек", st.DelayAfter)
			}
		}
		lines = append(lines, DiagnosticLine{lvl, fmt.Sprintf("Шаг %d", i+1), detail})
	}
	finalDetail := currentScenarioActionSummary()
	if dry {
		finalDetail = "Было бы выполнено: " + finalDetail + " (реальное действие заблокировано тестом)"
	}
	lines = append(lines, DiagnosticLine{diagInfo, "Финальное действие", finalDetail})
	return lines
}

// --- Wake timers -----------------------------------------------------------
var (
	wakeKernel32          = syscall.NewLazyDLL("kernel32.dll")
	pCreateWaitableTimerW = wakeKernel32.NewProc("CreateWaitableTimerW")
	pSetWaitableTimer     = wakeKernel32.NewProc("SetWaitableTimer")
	pCancelWaitableTimer  = wakeKernel32.NewProc("CancelWaitableTimer")
	wakeTimer             uintptr
	wakeTarget            time.Time
)

func nextScheduledWake(now time.Time) (time.Time, string, bool) {
	var best time.Time
	name := ""
	lead := time.Duration(clampInt(app.settings.WakeLeadMinutes, 0, 60)) * time.Minute
	for _, t := range app.settings.SavedTasks {
		if t.Paused || t.Mode != 4 || !t.Recurrence.Enabled {
			continue
		}
		occ, err := nextOccurrence(t.Recurrence, now)
		if err != nil {
			continue
		}
		target := occ.Add(-lead)
		if target.Before(now.Add(2 * time.Second)) {
			target = now.Add(2 * time.Second)
		}
		if best.IsZero() || target.Before(best) {
			best, name = target, t.Name
		}
	}
	return best, name, !best.IsZero()
}

func maintainWakeTimer(now time.Time) {
	if !app.settings.WakeScheduledTasks {
		cancelWakeTimer()
		return
	}
	target, name, ok := nextScheduledWake(now)
	if !ok {
		cancelWakeTimer()
		return
	}
	if !wakeTarget.IsZero() && abs(target.Sub(wakeTarget).Seconds()) < 1 {
		return
	}
	if wakeTimer == 0 {
		h, _, _ := pCreateWaitableTimerW.Call(0, 1, 0)
		if h == 0 {
			return
		}
		wakeTimer = h
	}
	// Positive absolute FILETIME: 100ns ticks since 1601-01-01 UTC.
	ft := (target.UTC().Unix()+11644473600)*10000000 + int64(target.UTC().Nanosecond()/100)
	r, _, err := pSetWaitableTimer.Call(wakeTimer, uintptr(unsafe.Pointer(&ft)), 0, 0, 0, 1)
	if r == 0 {
		appendHistory("ERROR", "Не удалось установить таймер пробуждения: "+err.Error())
		wakeTarget = time.Time{}
		return
	}
	wakeTarget = target
	appendHistory("WAKE_ARM", fmt.Sprintf("%s · %s", name, target.Format("02.01.2006 15:04")))
}

func cancelWakeTimer() {
	if wakeTimer != 0 {
		pCancelWaitableTimer.Call(wakeTimer)
	}
	wakeTarget = time.Time{}
}

func closeWakeTimer() {
	cancelWakeTimer()
	if wakeTimer != 0 {
		pCloseHandle.Call(wakeTimer)
		wakeTimer = 0
	}
}

// --- DWM rounded top-level window -----------------------------------------
var (
	dwmapi                 = syscall.NewLazyDLL("dwmapi.dll")
	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	dwmwaWindowCornerPreference = 33
	dwmwcpRound                 = 2
)

func applyRoundedWindowCorners(hwnd uintptr) {
	pref := uint32(dwmwcpRound)
	pDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&pref)), unsafe.Sizeof(pref))
}

func parseIntSafe(s string, d int) int {
	v, e := strconv.Atoi(strings.TrimSpace(s))
	if e != nil {
		return d
	}
	return v
}

func rebuildSavedFilter() []int {
	q := strings.ToLower(strings.TrimSpace(app.savedSearchText))
	fav, rest := []int{}, []int{}
	for i, t := range app.settings.SavedTasks {
		hay := strings.ToLower(t.Name + " " + savedTaskSummary(t))
		if q != "" && !strings.Contains(hay, q) {
			continue
		}
		if t.Favorite {
			fav = append(fav, i)
		} else {
			rest = append(rest, i)
		}
	}
	app.savedFilteredIndices = append(fav, rest...)
	return app.savedFilteredIndices
}

func savedUnderlyingIndex(local int) int {
	pos := app.savedScroll + local
	if pos < 0 || pos >= len(app.savedFilteredIndices) {
		return -1
	}
	return app.savedFilteredIndices[pos]
}

func savedLocalForUnderlying(idx int) int {
	for pos, v := range app.savedFilteredIndices {
		if v == idx {
			local := pos - app.savedScroll
			if local >= 0 && local < app.savedVisible {
				return local
			}
			return -1
		}
	}
	return -1
}

func toggleFavoriteTask(idx int) {
	if idx < 0 || idx >= len(app.settings.SavedTasks) {
		return
	}
	app.settings.SavedTasks[idx].Favorite = !app.settings.SavedTasks[idx].Favorite
	saveSettings()
	rebuildSavedFilter()
	appendHistory("EDIT", "Избранное: "+app.settings.SavedTasks[idx].Name)
}

func updateScenarioDragTarget(y int32) {
	if app.draggingScenarioKind == 1 {
		items := currentScenarioConditions()
		last := app.draggingScenarioIndex
		for slot, r := range app.conditionRows {
			idx := app.conditionRowIndices[slot]
			if idx < 0 || idx >= len(items) || r.Right <= r.Left {
				continue
			}
			last = idx
			if y < (r.Top+r.Bottom)/2 {
				app.draggingScenarioTarget = idx
				return
			}
		}
		app.draggingScenarioTarget = last
	} else if app.draggingScenarioKind == 2 {
		items := currentScenarioSteps()
		last := app.draggingScenarioIndex
		for slot, r := range app.stepRows {
			idx := app.stepRowIndices[slot]
			if idx < 0 || idx >= len(items) || r.Right <= r.Left {
				continue
			}
			last = idx
			if y < (r.Top+r.Bottom)/2 {
				app.draggingScenarioTarget = idx
				return
			}
		}
		app.draggingScenarioTarget = last
	}
}

func moveConditionTo(list []AutomationCondition, from, to int) []AutomationCondition {
	if from < 0 || to < 0 || from >= len(list) || to >= len(list) || from == to {
		return list
	}
	item := list[from]
	if from < to {
		copy(list[from:to], list[from+1:to+1])
	} else {
		copy(list[to+1:from+1], list[to:from])
	}
	list[to] = item
	if len(list) > 0 {
		list[0].Logic = logicAND
	}
	return list
}
func moveStepTo(list []ActionStep, from, to int) []ActionStep {
	if from < 0 || to < 0 || from >= len(list) || to >= len(list) || from == to {
		return list
	}
	item := list[from]
	if from < to {
		copy(list[from:to], list[from+1:to+1])
	} else {
		copy(list[to+1:from+1], list[to:from])
	}
	list[to] = item
	return list
}
func finishScenarioDrag() {
	kind, from, to := app.draggingScenarioKind, app.draggingScenarioIndex, app.draggingScenarioTarget
	app.draggingScenarioKind = 0
	app.draggingScenarioIndex, app.draggingScenarioTarget = -1, -1
	if from < 0 || to < 0 {
		return
	}
	if kind == 1 {
		list := append([]AutomationCondition(nil), currentScenarioConditions()...)
		if from >= len(list) || to >= len(list) {
			return
		}
		item := list[from]
		target := list[to]
		parentID := target.GroupID
		if target.Type == condGroup && target.ID != item.ID {
			parentID = target.ID
		}
		if item.Type != condGroup || !conditionGroupWouldCycle(list, item.ID, parentID) {
			list[from].GroupID = parentID
		}
		if from != to {
			list = moveConditionTo(list, from, to)
		}
		setCurrentScenarioConditions(list)
		resetConditionRuntimes()
		if !app.scenarioSavedDraft {
			appendHistory("EDIT", "Порядок условий изменён")
		}
		playUI(clickSound)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return
	}
	if kind == 2 {
		list := cloneActionSteps(currentScenarioSteps())
		if from >= len(list) || to >= len(list) {
			return
		}
		list = moveStepTo(list, from, to)
		setCurrentScenarioSteps(list)
		if !app.scenarioSavedDraft {
			appendHistory("EDIT", "Порядок шагов изменён")
		}
		playUI(clickSound)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	}
}
