//go:build windows

package main

import (
	"fmt"
	"sync"
)

func drawTemplates040(hdc uintptr, body RECT) {
	drawButton(hdc, app.templateBackRect, "← Назад", false)
	drawText(hdc, "Шаблоны задач", int(body.Left)+144, int(body.Top)+18, int(body.Right-body.Left)-166, 30, 19, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	ts := templates040()
	for i, r := range app.templateRects {
		if i >= len(ts) {
			continue
		}
		rv, hv := hoverCardRect(r)
		c := surfaceButtonColor()
		if hv > 0 {
			c = blendColor(c, theme.accent2, .07*hv)
		}
		roundFill(hdc, rv, c, 12)
		drawText(hdc, ts[i].Name, int(r.Left)+14, int(r.Top)+8, int(r.Right-r.Left)-28, 20, 12, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, ts[i].Description, int(r.Left)+14, int(r.Top)+29, int(r.Right-r.Left)-28, 20, 9, 400, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

type execVisual040 struct {
	sync.Mutex
	Running     bool
	CurrentStep int
	Done        map[int]bool
	Failed      map[int]bool
	Final       string
}

var execVisualState040 = execVisual040{CurrentStep: -1, Done: map[int]bool{}, Failed: map[int]bool{}}

func execVisualStart040() {
	execVisualState040.Lock()
	execVisualState040.Running = true
	execVisualState040.CurrentStep = -1
	execVisualState040.Done = map[int]bool{}
	execVisualState040.Failed = map[int]bool{}
	execVisualState040.Final = ""
	execVisualState040.Unlock()
}
func execVisualStep040(i int) {
	execVisualState040.Lock()
	execVisualState040.CurrentStep = i
	execVisualState040.Unlock()
	if app.hwnd != 0 {
		invalidate(app.hwnd)
	}
}
func execVisualDone040(i int, ok bool) {
	execVisualState040.Lock()
	if ok {
		execVisualState040.Done[i] = true
	} else {
		execVisualState040.Failed[i] = true
	}
	execVisualState040.Unlock()
	if app.hwnd != 0 {
		invalidate(app.hwnd)
	}
}
func execVisualFinal040(s string) {
	execVisualState040.Lock()
	execVisualState040.Running = false
	execVisualState040.CurrentStep = -1
	execVisualState040.Final = s
	execVisualState040.Unlock()
	if app.hwnd != 0 {
		invalidate(app.hwnd)
	}
}

type visualSnapshot040 struct {
	Running      bool
	Current      int
	Done, Failed map[int]bool
	Final        string
}

func getExecVisual040() visualSnapshot040 {
	execVisualState040.Lock()
	defer execVisualState040.Unlock()
	d := map[int]bool{}
	f := map[int]bool{}
	for k, v := range execVisualState040.Done {
		d[k] = v
	}
	for k, v := range execVisualState040.Failed {
		f[k] = v
	}
	return visualSnapshot040{execVisualState040.Running, execVisualState040.CurrentStep, d, f, execVisualState040.Final}
}

func drawScenarioPreview040(hdc uintptr, body RECT) {
	drawButton(hdc, app.previewBackRect, "← Назад", false)
	drawText(hdc, "Предпросмотр блок-схемы", int(body.Left)+144, int(body.Top)+18, int(body.Right-body.Left)-288, 30, 19, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	conds := currentScenarioConditions()
	steps := currentScenarioSteps()
	vs := getExecVisual040()
	idx := 0
	drawPreviewNode040(hdc, app.previewRows[idx], "Когда · "+currentScenarioWhenSummary(), previewStatusColor040(app.schedule.active, false, false))
	idx++
	for i, c := range conds {
		if idx >= len(app.previewRows) {
			break
		}
		ok, _ := diagnoseCondition(c)
		col := theme.muted
		if ok {
			col = theme.success
		} else if app.schedule.active {
			col = theme.accent2
		}
		drawPreviewNode040(hdc, app.previewRows[idx], fmt.Sprintf("Условие %d · %s", i+1, conditionSummary(c)), col)
		idx++
	}
	for i, st := range steps {
		if idx >= len(app.previewRows) {
			break
		}
		col := theme.muted
		if vs.Failed[i] {
			col = theme.danger
		} else if vs.Done[i] {
			col = theme.success
		} else if vs.Running && vs.Current == i {
			col = theme.accent2
		}
		drawPreviewNode040(hdc, app.previewRows[idx], fmt.Sprintf("Шаг %d · %s", i+1, stepSummary(st)), col)
		idx++
	}
	if idx < len(app.previewRows) {
		col := theme.muted
		if vs.Final == "ok" {
			col = theme.success
		} else if vs.Final == "error" {
			col = theme.danger
		}
		drawPreviewNode040(hdc, app.previewRows[idx], "Финальное действие · "+currentScenarioActionSummary(), col)
	}
}
func previewStatusColor040(active, done, failed bool) uint32 {
	if failed {
		return theme.danger
	}
	if done {
		return theme.success
	}
	if active {
		return theme.accent2
	}
	return theme.muted
}
func drawPreviewNode040(hdc uintptr, r RECT, label string, status uint32) {
	if r.Right <= r.Left {
		return
	}
	roundFill(hdc, r, surfaceButtonColor(), 11)
	roundFill(hdc, RECT{r.Left + 8, r.Top + 10, r.Left + 14, r.Bottom - 10}, status, 3)
	drawText(hdc, label, int(r.Left)+24, int(r.Top), int(r.Right-r.Left)-36, int(r.Bottom-r.Top), 11, 600, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}
