//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// AutomationCondition types.
const condGroup = -1

const (
	condCPU = iota
	condGPU
	condNetwork
	condDisk
	condFileStable
	condProcessExit
	condWindowExists
	condWindowMissing
	condWindowActive
	condWindowTitle
	condAudioSilent
	condBatteryPercent
	condACPower
	condDiskFree
	condFolderCount
	condProcessCPU
	condProcessGPU
	condProcessRAM
	condInternet
	condFullscreen
	condDrivePresent
)

// Condition.Logic describes how this condition is joined with the previous one.
const (
	logicAND = iota
	logicOR
)

// ActionStep types. The selected PowerPilot power action is always the final action.
const (
	stepCloseProcesses = iota
	stepWait
	stepRunCommand
	stepNotify
	stepMonitorOff
	stepMonitorOn
	stepSetVolume
	stepMute
	stepLockWorkstation
	stepPowerPlan
	stepProcessPriority
)

type AutomationCondition struct {
	ID          string  `json:"id"`
	Type        int     `json:"type"`
	Logic       int     `json:"logic"`
	Compare     int     `json:"compare"` // -1: <= threshold; 1: >= threshold
	Threshold   float64 `json:"threshold"`
	HoldSeconds int     `json:"hold_seconds"`
	Text        string  `json:"text"`
	Enabled     bool    `json:"enabled"`
	OpenGroups  int     `json:"open_groups,omitempty"`
	CloseGroups int     `json:"close_groups,omitempty"`
	DelayAfter  int     `json:"delay_after,omitempty"`
	GroupID     string  `json:"group_id,omitempty"` // parent compound condition; group blocks use their own ID as the node key
}

func migrateLegacyConditionGroups(src []AutomationCondition) []AutomationCondition {
	hasLegacy := false
	for _, c := range src {
		if c.OpenGroups > 0 || c.CloseGroups > 0 {
			hasLegacy = true
			break
		}
	}
	if !hasLegacy {
		return src
	}
	out := make([]AutomationCondition, 0, len(src)+4)
	stack := []string{}
	for _, original := range src {
		c := original
		openCount := clampInt(c.OpenGroups, 0, 3)
		closeCount := clampInt(c.CloseGroups, 0, 3)
		for i := 0; i < openCount; i++ {
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			logic := logicAND
			if i == 0 {
				logic = c.Logic
			}
			id := newAutomationID("group")
			out = append(out, AutomationCondition{ID: id, Type: condGroup, Logic: logic, Enabled: true, GroupID: parent})
			stack = append(stack, id)
		}
		if len(stack) > 0 {
			c.GroupID = stack[len(stack)-1]
		}
		if openCount > 0 {
			c.Logic = logicAND
		}
		c.OpenGroups, c.CloseGroups = 0, 0
		out = append(out, c)
		for i := 0; i < closeCount && len(stack) > 0; i++ {
			stack = stack[:len(stack)-1]
		}
	}
	return out
}

type ActionStep struct {
	ID         string   `json:"id"`
	Type       int      `json:"type"`
	Value      int      `json:"value"`
	Text       string   `json:"text"`
	Processes  []string `json:"processes,omitempty"`
	OnError    int      `json:"on_error,omitempty"` // 0 continue, 1 stop, 2 retry
	Retries    int      `json:"retries,omitempty"`
	DelayAfter int      `json:"delay_after,omitempty"`
}

func cloneActionSteps(src []ActionStep) []ActionStep {
	if len(src) == 0 {
		return nil
	}
	out := make([]ActionStep, len(src))
	copy(out, src)
	for i := range out {
		out[i].Processes = append([]string(nil), src[i].Processes...)
	}
	return out
}

type RecurrenceSpec struct {
	Enabled  bool    `json:"enabled"`
	Kind     int     `json:"kind"` // 0 daily, 1 weekdays, 2 selected weekdays
	TimeHHMM string  `json:"time_hhmm"`
	Days     [7]bool `json:"days"` // Monday..Sunday
}

type conditionRuntime struct {
	trueSince      time.Time
	lastSize       int64
	lastSizeChange time.Time
	lastFileCheck  time.Time
	fileInit       bool
	passedAt       time.Time
}

var (
	conditionRuntimeMu sync.Mutex
	conditionRuntimes  = map[string]*conditionRuntime{}
	automationIDSeq    atomic.Uint64
)

func newAutomationID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), automationIDSeq.Add(1))
}

func resetConditionRuntimes() {
	conditionRuntimeMu.Lock()
	conditionRuntimes = map[string]*conditionRuntime{}
	conditionRuntimeMu.Unlock()
}

func conditionName(t int) string {
	switch t {
	case condCPU:
		return "CPU"
	case condGPU:
		return "GPU"
	case condNetwork:
		return "Сеть"
	case condDisk:
		return "Диск"
	case condFileStable:
		return "Файл завершён"
	case condProcessExit:
		return "Процесс завершён"
	case condWindowExists:
		return "Окно существует"
	case condWindowMissing:
		return "Окно закрыто"
	case condWindowActive:
		return "Окно активно"
	case condWindowTitle:
		return "Заголовок окна"
	case condAudioSilent:
		return "Нет звука"
	case condBatteryPercent:
		return "Батарея"
	case condACPower:
		return "Источник питания"
	case condDiskFree:
		return "Свободное место"
	case condFolderCount:
		return "Файлы в папке"
	case condProcessCPU:
		return "CPU процесса"
	case condProcessGPU:
		return "GPU процесса"
	case condProcessRAM:
		return "RAM процесса"
	case condInternet:
		return "Подключение к сети"
	case condFullscreen:
		return "Полноэкранное приложение"
	case condDrivePresent:
		return "Диск / устройство"
	}
	return "Условие"
}

func conditionSummary(c AutomationCondition) string {
	hold := ""
	if c.HoldSeconds > 0 {
		hold = fmt.Sprintf(" · %d сек", c.HoldSeconds)
	}
	if c.DelayAfter > 0 {
		hold += fmt.Sprintf(" · пауза %d сек", c.DelayAfter)
	}
	op := "≤"
	if c.Compare > 0 {
		op = "≥"
	}
	switch c.Type {
	case condCPU, condGPU, condDisk:
		return fmt.Sprintf("%s %s %.0f%%%s", conditionName(c.Type), op, c.Threshold, hold)
	case condNetwork:
		return fmt.Sprintf("Сеть %s %.0f КБ/с%s", op, c.Threshold, hold)
	case condFileStable:
		return fmt.Sprintf("Файл стабилен: %s · %d сек", shortenMiddle(c.Text, 34), max(c.HoldSeconds, 5))
	case condProcessExit:
		return "Завершение " + shortenMiddle(c.Text, 38)
	case condWindowExists:
		return "Есть окно: " + shortenMiddle(c.Text, 38) + hold
	case condWindowMissing:
		return "Нет окна: " + shortenMiddle(c.Text, 38) + hold
	case condWindowActive:
		return "Активно окно: " + shortenMiddle(c.Text, 34) + hold
	case condWindowTitle:
		return "Заголовок содержит: " + shortenMiddle(c.Text, 30) + hold
	case condAudioSilent:
		if strings.TrimSpace(c.Text) != "" {
			return fmt.Sprintf("Нет звука: %s%s", shortenMiddle(c.Text, 28), hold)
		}
		return fmt.Sprintf("Нет системного звука%s", hold)
	case condBatteryPercent:
		return fmt.Sprintf("Батарея %s %.0f%%%s", op, c.Threshold, hold)
	case condACPower:
		if c.Compare > 0 {
			return "Питание от сети" + hold
		}
		return "Питание от батареи" + hold
	case condDiskFree:
		path := strings.TrimSpace(c.Text)
		if path == "" {
			path = `C:\`
		}
		return fmt.Sprintf("Свободно %s %.1f ГБ: %s%s", op, c.Threshold, shortenMiddle(path, 24), hold)
	case condFolderCount:
		return fmt.Sprintf("Файлов %s %.0f: %s%s", op, c.Threshold, shortenMiddle(c.Text, 24), hold)
	case condProcessCPU:
		return fmt.Sprintf("CPU %s %s %.0f%%%s", shortenMiddle(c.Text, 24), op, c.Threshold, hold)
	case condProcessGPU:
		return fmt.Sprintf("GPU %s %s %.0f%%%s", shortenMiddle(c.Text, 24), op, c.Threshold, hold)
	case condProcessRAM:
		return fmt.Sprintf("RAM %s %s %.0f МБ%s", shortenMiddle(c.Text, 24), op, c.Threshold, hold)
	case condInternet:
		if c.Compare > 0 {
			return "Интернет подключён" + hold
		}
		return "Интернет отключён" + hold
	case condFullscreen:
		if c.Compare > 0 {
			return "Есть полноэкранное приложение" + hold
		}
		return "Нет полноэкранного приложения" + hold
	case condDrivePresent:
		state := "отключён"
		if c.Compare > 0 {
			state = "подключён"
		}
		return fmt.Sprintf("Диск/устройство %s: %s%s", state, shortenMiddle(c.Text, 28), hold)
	}
	return conditionName(c.Type)
}

func stepName(t int) string {
	switch t {
	case stepCloseProcesses:
		return "Закрыть процессы"
	case stepWait:
		return "Подождать"
	case stepRunCommand:
		return "Запустить программу / команду"
	case stepNotify:
		return "Уведомление"
	case stepMonitorOff:
		return "Выключить монитор"
	case stepMonitorOn:
		return "Включить монитор"
	case stepSetVolume:
		return "Громкость"
	case stepMute:
		return "Громкость / звук"
	case stepLockWorkstation:
		return "Заблокировать ПК"
	case stepPowerPlan:
		return "План питания"
	case stepProcessPriority:
		return "Приоритет процесса"
	}
	return "Действие"
}

func stepSummary(s ActionStep) string {
	suffix := ""
	if s.DelayAfter > 0 {
		suffix = fmt.Sprintf(" · +%d сек", s.DelayAfter)
	}
	switch s.Type {
	case stepCloseProcesses:
		return "Закрыть " + processCountPhrase(len(s.Processes)) + suffix
	case stepWait:
		return fmt.Sprintf("Подождать %d сек%s", max(s.Value, 1), suffix)
	case stepRunCommand:
		return "Запустить: " + shortenMiddle(s.Text, 42) + suffix
	case stepNotify:
		txt := strings.TrimSpace(s.Text)
		if txt == "" {
			txt = "Сценарий PowerPilot продолжается"
		}
		return "Уведомить: " + shortenMiddle(txt, 42) + suffix
	case stepMonitorOff:
		return "Выключить монитор" + suffix
	case stepMonitorOn:
		return "Включить монитор" + suffix
	case stepSetVolume:
		return fmt.Sprintf("Громкость %d%%%s", clampInt(s.Value, 0, 100), suffix)
	case stepMute:
		state := "Выключить звук"
		if s.Value == 0 {
			state = "Включить звук"
		}
		return state + suffix
	case stepLockWorkstation:
		return "Заблокировать ПК" + suffix
	case stepPowerPlan:
		names := []string{"Энергосбережение", "Сбалансированный", "Высокая производительность"}
		v := clampInt(s.Value, 0, 2)
		return "План питания: " + names[v] + suffix
	case stepProcessPriority:
		names := []string{"Низкий", "Обычный", "Высокий"}
		v := clampInt(s.Value, 0, 2)
		return fmt.Sprintf("Приоритет %s: %s%s", shortenMiddle(s.Text, 26), names[v], suffix)
	}
	return stepName(s.Type)
}

func shortenMiddle(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n || n < 7 {
		return s
	}
	a := (n - 1) / 2
	b := n - 1 - a
	return string(r[:a]) + "…" + string(r[len(r)-b:])
}

func recurrenceSummary(r RecurrenceSpec) string {
	tm := r.TimeHHMM
	if tm == "" {
		tm = "23:00"
	}
	switch r.Kind {
	case 1:
		return "Будни · " + tm
	case 2:
		names := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}
		var d []string
		for i, ok := range r.Days {
			if ok {
				d = append(d, names[i])
			}
		}
		if len(d) == 0 {
			return "Выбранные дни · " + tm
		}
		return strings.Join(d, ", ") + " · " + tm
	default:
		return "Каждый день · " + tm
	}
}

func parseHHMM(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	p := strings.Split(s, ":")
	if len(p) != 2 {
		return 0, 0, fmt.Errorf("bad time")
	}
	h, e1 := strconv.Atoi(p[0])
	m, e2 := strconv.Atoi(p[1])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("bad time")
	}
	return h, m, nil
}

func recurrenceAllowedDay(r RecurrenceSpec, t time.Time) bool {
	// Go: Sunday=0. PowerPilot: Monday=0.
	idx := (int(t.Weekday()) + 6) % 7
	switch r.Kind {
	case 1:
		return idx <= 4
	case 2:
		return r.Days[idx]
	default:
		return true
	}
}

func nextOccurrence(r RecurrenceSpec, from time.Time) (time.Time, error) {
	h, m, err := parseHHMM(r.TimeHHMM)
	if err != nil {
		return time.Time{}, err
	}
	loc := from.Location()
	for i := 0; i < 8; i++ {
		d := from.AddDate(0, 0, i)
		c := time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
		if !recurrenceAllowedDay(r, c) {
			continue
		}
		if c.After(from) {
			return c, nil
		}
	}
	return time.Time{}, fmt.Errorf("no occurrence")
}

func recurrenceKey(t time.Time) string { return t.Format("2006-01-02 15:04") }

func savedRecurrenceDue(t SavedTask, now time.Time) (string, bool) {
	if t.Paused {
		return "", false
	}
	r := t.Recurrence
	if !r.Enabled || t.Mode != 4 {
		return "", false
	}
	h, m, err := parseHHMM(r.TimeHHMM)
	if err != nil || now.Hour() != h || now.Minute() != m || !recurrenceAllowedDay(r, now) {
		return "", false
	}
	key := recurrenceKey(now)
	return key, key != t.LastRunKey
}

func evaluateAutomationConditions(conds []AutomationCondition) (bool, string) {
	enabled := make([]AutomationCondition, 0, len(conds))
	for _, c := range conds {
		if c.Enabled {
			enabled = append(enabled, c)
		}
	}
	if len(enabled) == 0 {
		return true, ""
	}
	leafConditions := make([]AutomationCondition, 0, len(enabled))
	values := make([]bool, 0, len(enabled))
	details := make([]string, 0, len(enabled))
	valuesByID := make(map[string]bool, len(enabled))
	hasGroups := false
	for _, c := range enabled {
		if c.Type == condGroup {
			hasGroups = true
			continue
		}
		ok, d := evaluateOneCondition(c)
		leafConditions = append(leafConditions, c)
		values = append(values, ok)
		details = append(details, d)
		valuesByID[c.ID] = ok
	}
	result := evalGroupedConditionValues(leafConditions, values)
	if hasGroups {
		result, _ = evalCompoundConditionValues(enabled, valuesByID, "", map[string]bool{})
	}
	if result {
		return true, ""
	}
	for i, ok := range values {
		if !ok && details[i] != "" {
			return false, details[i]
		}
	}
	return false, "условия сценария ещё не выполнены"
}

func evalCompoundConditionValues(conds []AutomationCondition, values map[string]bool, parentID string, visiting map[string]bool) (bool, bool) {
	result, found := false, false
	for _, c := range conds {
		if !c.Enabled || c.GroupID != parentID {
			continue
		}
		value := false
		if c.Type == condGroup {
			if visiting[c.ID] {
				continue
			}
			visiting[c.ID] = true
			value, _ = evalCompoundConditionValues(conds, values, c.ID, visiting)
			delete(visiting, c.ID)
		} else {
			value = values[c.ID]
		}
		if !found {
			result, found = value, true
		} else if c.Logic == logicOR {
			result = result || value
		} else {
			result = result && value
		}
	}
	return result, found
}

func evalGroupedConditionValues(conds []AutomationCondition, values []bool) bool {
	if len(conds) == 0 || len(values) == 0 {
		return true
	}
	type token struct {
		kind  int
		value bool
		op    int
	}
	// kind: 0 value, 1 op, 2 open, 3 close
	var tokens []token
	balance := 0
	for i, c := range conds {
		if i > 0 {
			tokens = append(tokens, token{kind: 1, op: c.Logic})
		}
		for j := 0; j < clampInt(c.OpenGroups, 0, 3); j++ {
			tokens = append(tokens, token{kind: 2})
			balance++
		}
		tokens = append(tokens, token{kind: 0, value: values[i]})
		for j := 0; j < clampInt(c.CloseGroups, 0, 3) && balance > 0; j++ {
			tokens = append(tokens, token{kind: 3})
			balance--
		}
	}
	for balance > 0 {
		tokens = append(tokens, token{kind: 3})
		balance--
	}
	vals := []bool{}
	ops := []int{}
	apply := func() {
		if len(vals) < 2 || len(ops) == 0 {
			return
		}
		b := vals[len(vals)-1]
		a := vals[len(vals)-2]
		vals = vals[:len(vals)-2]
		op := ops[len(ops)-1]
		ops = ops[:len(ops)-1]
		if op == logicOR {
			vals = append(vals, a || b)
		} else {
			vals = append(vals, a && b)
		}
	}
	// Parentheses have priority; AND/OR intentionally retain PowerPilot's left-to-right semantics inside each group.
	marker := -99
	for _, t := range tokens {
		switch t.kind {
		case 0:
			vals = append(vals, t.value)
		case 1:
			for len(ops) > 0 && ops[len(ops)-1] != marker {
				apply()
			}
			ops = append(ops, t.op)
		case 2:
			ops = append(ops, marker)
		case 3:
			for len(ops) > 0 && ops[len(ops)-1] != marker {
				apply()
			}
			if len(ops) > 0 && ops[len(ops)-1] == marker {
				ops = ops[:len(ops)-1]
			}
		}
	}
	for len(ops) > 0 {
		apply()
	}
	if len(vals) == 0 {
		return true
	}
	return vals[len(vals)-1]
}

func evaluateOneCondition(c AutomationCondition) (bool, string) {
	conditionRuntimeMu.Lock()
	st := conditionRuntimes[c.ID]
	if st == nil {
		st = &conditionRuntime{}
		conditionRuntimes[c.ID] = st
	}
	conditionRuntimeMu.Unlock()

	var raw bool
	var current float64
	metric := metricsSnapshot()
	switch c.Type {
	case condCPU:
		current = metric.CPU
		raw = compareMetric(current, c.Threshold, c.Compare)
	case condGPU:
		current = metric.GPU
		raw = metric.GPU >= 0 && compareMetric(current, c.Threshold, c.Compare)
	case condNetwork:
		current = metric.NetworkKBps
		raw = compareMetric(current, c.Threshold, c.Compare)
	case condDisk:
		current = metric.DiskPercent
		raw = metric.DiskPercent >= 0 && compareMetric(current, c.Threshold, c.Compare)
	case condBatteryPercent:
		current = metric.BatteryPercent
		raw = current >= 0 && compareMetric(current, c.Threshold, c.Compare)
	case condACPower:
		raw = metric.OnAC == (c.Compare > 0)
	case condDiskFree:
		free, _, _, ok := diskFreeInfo(c.Text)
		current = free
		raw = ok && compareMetric(current, c.Threshold, c.Compare)
	case condFolderCount:
		entries, err := os.ReadDir(strings.TrimSpace(c.Text))
		if err == nil {
			count := 0
			for _, entry := range entries {
				if !entry.IsDir() {
					count++
				}
			}
			current = float64(count)
			raw = compareMetric(current, c.Threshold, c.Compare)
		}
	case condProcessCPU, condProcessGPU, condProcessRAM:
		if pm, ok := processMetricByName(c.Text); ok {
			switch c.Type {
			case condProcessCPU:
				current = pm.CPUPercent
			case condProcessGPU:
				current = pm.GPUPercent
			case condProcessRAM:
				current = pm.RAMMB
			}
			raw = current >= 0 && compareMetric(current, c.Threshold, c.Compare)
		}
	case condInternet:
		raw = internetConnected040() == (c.Compare > 0)
	case condFullscreen:
		raw = foregroundFullscreen() == (c.Compare > 0)
	case condDrivePresent:
		path := strings.TrimSpace(c.Text)
		exists := false
		if path != "" {
			_, err := os.Stat(path)
			exists = err == nil
		}
		raw = exists == (c.Compare > 0)
	case condProcessExit:
		raw = strings.TrimSpace(c.Text) != "" && !processRunning(c.Text)
	case condWindowExists:
		raw = strings.TrimSpace(c.Text) != "" && windowMatch040(c.Text, false, false)
	case condWindowMissing:
		raw = strings.TrimSpace(c.Text) != "" && !windowMatch040(c.Text, false, false)
	case condWindowActive:
		raw = strings.TrimSpace(c.Text) != "" && windowMatch040(c.Text, true, false)
	case condWindowTitle:
		raw = strings.TrimSpace(c.Text) != "" && windowMatch040(c.Text, false, true)
	case condAudioSilent:
		peak := float32(-1)
		if strings.TrimSpace(c.Text) != "" {
			if p, found := audioPeakForProcess040(c.Text); found {
				peak = p
			}
		} else {
			peak = audioPeak040()
		}
		raw = peak >= 0 && peak < 0.008
		current = float64(peak * 100)
	case condFileStable:
		path := strings.TrimSpace(c.Text)
		now := time.Now()
		need := time.Duration(max(c.HoldSeconds, 5)) * time.Second
		// Directory walks can be expensive; sample at most once per second.
		if !st.lastFileCheck.IsZero() && now.Sub(st.lastFileCheck) < time.Second {
			return false, fmt.Sprintf("Проверяю стабильность файла · %s / %s", formatDuration(now.Sub(st.lastSizeChange)), formatDuration(need))
		}
		st.lastFileCheck = now
		size, ok := pathSize(path)
		if !ok {
			st.fileInit = false
			st.passedAt = time.Time{}
			return false, "Файл или папка пока недоступны"
		}
		if !st.fileInit {
			st.fileInit = true
			st.lastSize = size
			st.lastSizeChange = now
			st.passedAt = time.Time{}
			return false, "Ожидаю стабилизации файла"
		}
		if size != st.lastSize {
			st.lastSize = size
			st.lastSizeChange = now
			st.passedAt = time.Time{}
			return false, "Размер файла ещё изменяется"
		}
		if now.Sub(st.lastSizeChange) < need {
			st.passedAt = time.Time{}
			return false, fmt.Sprintf("Файл стабилен %s из %s", formatDuration(now.Sub(st.lastSizeChange)), formatDuration(need))
		}
		return conditionPostDelay(c, st)
	}

	if conditionMeasured(c.Type) {
		unit := "%"
		if c.Type == condNetwork {
			unit = " КБ/с"
		}
		if c.Type == condAudioSilent {
			unit = "% peak"
		}
		if c.Type == condDiskFree {
			unit = " ГБ"
		}
		if c.Type == condFolderCount {
			unit = " файлов"
		}
		if c.Type == condProcessRAM {
			unit = " МБ"
		}
		if !raw {
			st.trueSince = time.Time{}
			st.passedAt = time.Time{}
			return false, fmt.Sprintf("%s сейчас %.1f%s", conditionName(c.Type), current, unit)
		}
	} else if !raw {
		st.trueSince = time.Time{}
		st.passedAt = time.Time{}
		return false, conditionName(c.Type) + " ещё не выполнено"
	}

	if c.HoldSeconds <= 0 {
		st.trueSince = time.Time{}
		return conditionPostDelay(c, st)
	}
	now := time.Now()
	if st.trueSince.IsZero() {
		st.trueSince = now
		return false, "Условие должно удерживаться"
	}
	held := now.Sub(st.trueSince)
	need := time.Duration(c.HoldSeconds) * time.Second
	if held < need {
		st.passedAt = time.Time{}
		return false, fmt.Sprintf("Условие удерживается %s / %s", formatDuration(held), formatDuration(need))
	}
	return conditionPostDelay(c, st)
}

func conditionPostDelay(c AutomationCondition, st *conditionRuntime) (bool, string) {
	if c.DelayAfter <= 0 {
		st.passedAt = time.Time{}
		return true, ""
	}
	now := time.Now()
	if st.passedAt.IsZero() {
		st.passedAt = now
		return false, fmt.Sprintf("Пауза после условия 00:00:00 / %s", formatDuration(time.Duration(c.DelayAfter)*time.Second))
	}
	need := time.Duration(c.DelayAfter) * time.Second
	elapsed := now.Sub(st.passedAt)
	if elapsed < need {
		return false, fmt.Sprintf("Пауза после условия %s / %s", formatDuration(elapsed), formatDuration(need))
	}
	return true, ""
}

func conditionUsesThreshold(t int) bool {
	switch t {
	case condCPU, condGPU, condNetwork, condDisk, condBatteryPercent, condDiskFree, condFolderCount, condProcessCPU, condProcessGPU, condProcessRAM:
		return true
	}
	return false
}

func conditionMeasured(t int) bool {
	return conditionUsesThreshold(t) || t == condAudioSilent
}

func compareMetric(v, threshold float64, compare int) bool {
	if compare > 0 {
		return v >= threshold
	}
	return v <= threshold
}

func pathSize(path string) (int64, bool) {
	if path == "" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	if !info.IsDir() {
		return info.Size(), true
	}
	var total int64
	err = filepath.Walk(path, func(_ string, i os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !i.IsDir() {
			total += i.Size()
		}
		return nil
	})
	return total, err == nil
}

func executeScenarioSteps(s Schedule) bool {
	if s.runID != "" && !scheduleRunIsCurrent(s.runID) {
		return false
	}
	if s.closeBefore {
		for _, p := range s.processes {
			if !closeProcess(p, 8*time.Second) {
				appendRunHistory("ERROR", "Не удалось закрыть процесс: "+p, s.runID)
			}
		}
	}
	for idx, step := range s.steps {
		if s.runID != "" && !scheduleRunIsCurrent(s.runID) {
			return false
		}
		if s.runID != "" && !waitForScheduleResume(s.runID) {
			return false
		}
		execVisualStep040(idx)
		attempts := 1
		if step.OnError == 2 {
			attempts += clampInt(step.Retries, 0, 10)
		}
		var err error
		for attempt := 1; attempt <= attempts; attempt++ {
			err = executeOneScenarioStep(step, s)
			if err == nil {
				appendRunHistory("STEP", fmt.Sprintf("Шаг %d выполнен: %s", idx+1, stepSummary(step)), s.runID)
				break
			}
			appendRunHistory("ERROR", fmt.Sprintf("Шаг %d, попытка %d/%d: %v", idx+1, attempt, attempts, err), s.runID)
			if attempt < attempts {
				time.Sleep(400 * time.Millisecond)
			}
		}
		execVisualDone040(idx, err == nil)
		if err != nil && step.OnError == 1 {
			appendRunHistory("STEP_STOP", fmt.Sprintf("Сценарий остановлен после ошибки шага %d", idx+1), s.runID)
			return false
		}
		if step.DelayAfter > 0 {
			appendRunHistory("STEP", fmt.Sprintf("Пауза после шага %d: %d сек", idx+1, step.DelayAfter), s.runID)
			wait := time.Duration(step.DelayAfter) * time.Second
			if s.runID == "" {
				time.Sleep(wait)
			} else if !sleepScheduleAware(s.runID, wait) {
				return false
			}
			if s.runID != "" && !scheduleRunIsCurrent(s.runID) {
				return false
			}
		}
	}
	return true
}

func executeOneScenarioStep(step ActionStep, s Schedule) error {
	switch step.Type {
	case stepCloseProcesses:
		failed := []string{}
		for _, p := range step.Processes {
			if !closeProcess(p, 8*time.Second) {
				failed = append(failed, p)
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("не закрыты: %s", strings.Join(failed, ", "))
		}
	case stepWait:
		wait := time.Duration(max(step.Value, 1)) * time.Second
		if s.runID == "" {
			time.Sleep(wait)
		} else if !sleepScheduleAware(s.runID, wait) {
			return fmt.Errorf("задача отменена во время ожидания")
		}
	case stepRunCommand:
		cmdLine := strings.TrimSpace(step.Text)
		if cmdLine == "" {
			return fmt.Errorf("команда не задана")
		}
		literalPath := cmdLine
		if len(literalPath) >= 2 && literalPath[0] == '"' && literalPath[len(literalPath)-1] == '"' {
			literalPath = literalPath[1 : len(literalPath)-1]
		}
		var c *exec.Cmd
		// The file picker stores a literal path. Passing that path through cmd.exe
		// breaks on spaces and shell metacharacters, so launch real executables
		// directly. Free-form commands still use the command processor.
		if info, statErr := os.Stat(literalPath); statErr == nil && !info.IsDir() &&
			(strings.EqualFold(filepath.Ext(literalPath), ".exe") || strings.EqualFold(filepath.Ext(literalPath), ".com")) {
			c = exec.Command(literalPath)
		} else if statErr == nil && !info.IsDir() {
			// Documents, shortcuts and scripts selected through the picker should
			// use their normal Windows association instead of being parsed as an
			// unquoted command line.
			shellExecute := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
			result, _, callErr := shellExecute.Call(0, uintptr(unsafe.Pointer(wstr("open"))), uintptr(unsafe.Pointer(wstr(literalPath))), 0, 0, SW_SHOW)
			if result <= 32 {
				return fmt.Errorf("Windows не удалось открыть файл (ShellExecute=%d): %v", result, callErr)
			}
			return nil
		} else {
			c = exec.Command("cmd.exe", "/D", "/S", "/C", cmdLine)
		}
		c.SysProcAttr = hiddenProcAttr()
		if err := c.Start(); err != nil {
			return err
		}
	case stepNotify:
		txt := strings.TrimSpace(step.Text)
		if txt == "" {
			txt = "Сценарий PowerPilot продолжает выполнение."
		}
		showWindowsNotification("PowerPilot", txt)
	case stepMonitorOff:
		monitorPower040(false)
	case stepMonitorOn:
		monitorPower040(true)
	case stepSetVolume:
		if !setMasterVolume040(float32(clampInt(step.Value, 0, 100)) / 100) {
			return fmt.Errorf("не удалось изменить системную громкость")
		}
	case stepMute:
		if !setMasterMute040(step.Value != 0) {
			return fmt.Errorf("не удалось изменить mute")
		}
	case stepLockWorkstation:
		if !lockWorkstation040() {
			return fmt.Errorf("не удалось заблокировать рабочую станцию")
		}
	case stepPowerPlan:
		aliases := []string{"SCHEME_MAX", "SCHEME_BALANCED", "SCHEME_MIN"}
		v := clampInt(step.Value, 0, 2)
		c := exec.Command("powercfg.exe", "/setactive", aliases[v])
		c.SysProcAttr = hiddenProcAttr()
		if err := c.Run(); err != nil {
			return fmt.Errorf("не удалось сменить план питания: %w", err)
		}
	case stepProcessPriority:
		if strings.TrimSpace(step.Text) == "" {
			return fmt.Errorf("процесс не задан")
		}
		if !setProcessPriority040(step.Text, clampInt(step.Value, 0, 2)) {
			return fmt.Errorf("не удалось изменить приоритет процесса")
		}
	}
	return nil
}

func autoSavedScheduleTick(now time.Time) {
	if app.schedule.active {
		return
	}
	for i := range app.settings.SavedTasks {
		t := app.settings.SavedTasks[i]
		key, due := savedRecurrenceDue(t, now)
		if !due {
			continue
		}
		app.settings.SavedTasks[i].LastRunKey = key
		saveSettings()
		startSavedAutomation(t, now)
		return
	}
}

func startSavedAutomation(t SavedTask, now time.Time) {
	graph := ScenarioGraph{}
	conditions := append([]AutomationCondition(nil), t.Conditions...)
	steps := cloneActionSteps(t.Steps)
	if t.TaskKind == 1 {
		graph = ensureScenarioGraph(cloneScenarioGraph(t.Graph), taskStateFromSaved040(t))
		if scenarioGraphValidationError(graph) != "" {
			appendHistory("ERROR", "Некорректная схема автозапуска: "+t.Name)
			return
		}
		conditions, steps = nil, nil
	}
	s := Schedule{
		active: true, action: t.Action, mode: 4, started: now, target: now, runID: newRunID(),
		total: 0, sourceTaskID: t.ID, sourceTaskName: t.Name,
		conditions:     conditions,
		triggerLogic:   t.TriggerLogic,
		steps:          steps,
		closeBefore:    t.CloseBefore,
		processes:      append([]string(nil), t.Processes...),
		warningSeconds: t.WarningSeconds,
		graph:          graph,
	}
	app.schedule = s
	resetConditionRuntimes()
	app.status = "Автозапуск: " + t.Name
	app.countdown = "ГОТОВО"
	appendRunHistory("AUTO_START", t.Name, s.runID)
	showNotification("PowerPilot", "Запущена сохранённая задача: "+t.Name)
	invalidate(app.hwnd)
}

func hiddenProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
