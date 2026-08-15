//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxNotificationItems = 200

const (
	notifInfo = iota
	notifSuccess
	notifWarning
	notifError
	notifUpdate
)

const (
	notifTargetNone = iota
	notifTargetHistory
	notifTargetSensors
	notifTargetResources
	notifTargetData
)

type AppNotification struct {
	ID        string    `json:"id"`
	When      time.Time `json:"when"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
	Kind      int       `json:"kind"`
	Target    int       `json:"target"`
	Unread    bool      `json:"unread"`
	UniqueKey string    `json:"unique_key,omitempty"`
}

var notificationCenter struct {
	sync.Mutex
	Items []AppNotification
}

func notificationsPath() string {
	return filepath.Join(settingsDir(), "notifications.json")
}

func loadNotificationCenter() {
	notificationCenter.Lock()
	defer notificationCenter.Unlock()
	notificationCenter.Items = nil
	b, err := os.ReadFile(notificationsPath())
	if err == nil {
		_ = json.Unmarshal(b, &notificationCenter.Items)
	}
	// Defensive cleanup: malformed/empty rows should never occupy the badge forever.
	out := notificationCenter.Items[:0]
	for _, n := range notificationCenter.Items {
		if strings.TrimSpace(n.Title) == "" && strings.TrimSpace(n.Text) == "" {
			continue
		}
		if n.ID == "" {
			n.ID = fmt.Sprintf("legacy-%d", n.When.UnixNano())
		}
		out = append(out, n)
	}
	notificationCenter.Items = out
	sort.SliceStable(notificationCenter.Items, func(i, j int) bool {
		return notificationCenter.Items[i].When.After(notificationCenter.Items[j].When)
	})
	if len(notificationCenter.Items) > maxNotificationItems {
		notificationCenter.Items = notificationCenter.Items[:maxNotificationItems]
	}
}

func saveNotificationCenterLocked() {
	_ = os.MkdirAll(settingsDir(), 0755)
	b, err := json.MarshalIndent(notificationCenter.Items, "", "  ")
	if err == nil {
		_ = os.WriteFile(notificationsPath(), b, 0644)
	}
}

func pushAppNotification(kind int, title, text string, target int) {
	pushAppNotificationUnique("", kind, title, text, target)
}

func pushAppNotificationUnique(uniqueKey string, kind int, title, text string, target int) {
	title = strings.TrimSpace(title)
	text = strings.TrimSpace(text)
	if title == "" && text == "" {
		return
	}
	notificationCenter.Lock()
	if uniqueKey != "" {
		for i := range notificationCenter.Items {
			if notificationCenter.Items[i].UniqueKey == uniqueKey {
				// A three-hour scan must not resurrect the same update badge after the user
				// has already read it. Refresh metadata in place and keep its read state/time.
				notificationCenter.Items[i].Title = title
				notificationCenter.Items[i].Text = text
				notificationCenter.Items[i].Kind = kind
				notificationCenter.Items[i].Target = target
				saveNotificationCenterLocked()
				notificationCenter.Unlock()
				postNotificationCenterChanged()
				return
			}
		}
	}
	now := time.Now()
	n := AppNotification{
		ID: fmt.Sprintf("%d", now.UnixNano()), When: now, Title: title, Text: text,
		Kind: kind, Target: target, Unread: true, UniqueKey: uniqueKey,
	}
	notificationCenter.Items = append([]AppNotification{n}, notificationCenter.Items...)
	if len(notificationCenter.Items) > maxNotificationItems {
		notificationCenter.Items = notificationCenter.Items[:maxNotificationItems]
	}
	saveNotificationCenterLocked()
	notificationCenter.Unlock()
	postNotificationCenterChanged()
}

func postNotificationCenterChanged() {
	if app.hwnd != 0 {
		pPostMessageW.Call(app.hwnd, WM_NOTIFICATION_CHANGED, 0, 0)
	}
}

func notificationUnreadCount() int {
	notificationCenter.Lock()
	defer notificationCenter.Unlock()
	n := 0
	for _, it := range notificationCenter.Items {
		if it.Unread {
			n++
		}
	}
	return n
}

func notificationItemsSnapshot(unreadOnly bool) []AppNotification {
	notificationCenter.Lock()
	defer notificationCenter.Unlock()
	out := make([]AppNotification, 0, len(notificationCenter.Items))
	for _, it := range notificationCenter.Items {
		if unreadOnly && !it.Unread {
			continue
		}
		out = append(out, it)
	}
	return out
}

func markNotificationRead(id string) {
	if id == "" {
		return
	}
	notificationCenter.Lock()
	for i := range notificationCenter.Items {
		if notificationCenter.Items[i].ID == id {
			notificationCenter.Items[i].Unread = false
			break
		}
	}
	saveNotificationCenterLocked()
	notificationCenter.Unlock()
	postNotificationCenterChanged()
}

func markAllNotificationsRead() {
	notificationCenter.Lock()
	for i := range notificationCenter.Items {
		notificationCenter.Items[i].Unread = false
	}
	saveNotificationCenterLocked()
	notificationCenter.Unlock()
	postNotificationCenterChanged()
}

func clearNotifications() {
	notificationCenter.Lock()
	notificationCenter.Items = nil
	saveNotificationCenterLocked()
	notificationCenter.Unlock()
	postNotificationCenterChanged()
}

func notificationKindColor(kind int) uint32 {
	switch kind {
	case notifSuccess:
		return theme.success
	case notifWarning:
		return blendColor(theme.accent2, theme.danger, .35)
	case notifError:
		return theme.danger
	case notifUpdate:
		return theme.accent2
	default:
		return theme.muted
	}
}

func notificationTimeLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if now.Sub(t) < time.Minute && now.After(t) {
		return "сейчас"
	}
	if now.Year() == t.Year() && now.YearDay() == t.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("02.01 15:04")
}

func drawNotificationButton(hdc uintptr) {
	r := app.notificationBtnRect
	if r.Right <= r.Left {
		return
	}
	c := surfaceButtonColor()
	if app.notificationPanelOpen {
		c = blendColor(c, theme.accent, .42)
	}
	h := hoverAmount(r)
	if h > 0 {
		c = blendColor(c, theme.accent2, .10*h)
	}
	rv := expandRect(r, int32(2*h+.5))
	roundFill(hdc, rv, c, 12)
	if h > 0 && ui2d.active {
		d2dDrawRoundedOutline(rv, 12, float32(1+.35*h), blendColor(theme.border, theme.accent2, .42))
	}

	// Slightly larger than the original 24px bell. The extra hover scale is deliberately
	// tiny so the icon feels alive without colliding with the unread badge.
	iconSize := int32(28 + int32(2*h+.5))
	// Center against the animated button rectangle.  d2dDrawBellIconRotated now
	// scales its cached 24px bitmap to this destination rect around the same centre,
	// so no manual X/Y compensation is needed (the old +1px correction only masked
	// the brush-scaling bug and made the true centre inconsistent across hover).
	visualRect := rv
	x := visualRect.Left + (visualRect.Right-visualRect.Left-iconSize)/2
	y := visualRect.Top + (visualRect.Bottom-visualRect.Top-iconSize)/2
	iconRect := RECT{x, y, x + iconSize, y + iconSize}

	// One short "ring burst" on hover-entry or when a new unread notification arrives.
	// Particles stay clipped to the button and fade before they can touch adjacent controls.
	burst := 1.0
	burstActive := false
	if !app.notificationBellBurstStarted.IsZero() && app.settings.AnimationMode != 2 {
		burst = float64(time.Since(app.notificationBellBurstStarted)) / float64(560*time.Millisecond)
		if burst < 0 {
			burst = 0
		}
		if burst < 1 {
			burstActive = true
		}
	}
	if burstActive && ui2d.active {
		d2dPushClip(visualRect)
		cx := float32((visualRect.Left + visualRect.Right) / 2)
		cy := float32((visualRect.Top + visualRect.Bottom) / 2)
		// Fast-out travel and a soft fade make the dots read as a tiny outward sparkle.
		travel := 5.0 + 11.0*(1-math.Pow(1-burst, 2))
		opacity := float32(math.Pow(1-burst, 1.45) * .78)
		gold := rgb(255, 211, 55)
		warm := rgb(255, 239, 150)
		dirs := [][2]float64{{1, 0}, {.72, .72}, {0, 1}, {-.72, .72}, {-1, 0}, {-.72, -.72}, {0, -1}, {.72, -.72}}
		for i, d := range dirs {
			dist := travel
			if i%2 == 1 {
				dist *= .82
			}
			px := cx + float32(d[0]*dist)
			py := cy + float32(d[1]*dist)
			radius := float32(1.15)
			col := gold
			if i%2 == 1 {
				radius = .85
				col = warm
			}
			d2dFillEllipseOpacity(px, py, radius, radius, col, opacity)
		}
		d2dPopClip()
	}

	angle := 0.0
	if burstActive {
		angle = math.Sin(burst*math.Pi*6) * (1 - burst) * 5.0
	}
	d2dDrawBellIconRotated(iconRect, angle)

	unread := notificationUnreadCount()
	if unread > 0 {
		label := fmt.Sprintf("%d", unread)
		if unread > 99 {
			label = "99+"
		}
		badgeW := int32(18)
		if len(label) > 2 {
			badgeW = 24
		}
		badge := RECT{visualRect.Right - badgeW + 4, visualRect.Bottom - 15, visualRect.Right + 4, visualRect.Bottom + 3}
		roundFill(hdc, badge, theme.danger, 9)
		drawText(hdc, label, int(badge.Left), int(badge.Top), int(badge.Right-badge.Left), int(badge.Bottom-badge.Top), 9, 750, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func drawNotificationPanel(hdc uintptr) {
	if !app.notificationPanelOpen || app.notificationPanelRect.Right <= app.notificationPanelRect.Left {
		return
	}
	drawingNotificationPanel = true
	defer func() { drawingNotificationPanel = false }()
	p := app.notificationPanelRect
	// A compact floating surface: it overlays the active page instead of navigating away.
	if ui2d.active {
		d2dFillRoundedOpacity(expandRect(p, 4), blendColor(theme.bg, theme.panel2, .35), 18, .42)
	}
	roundFill(hdc, p, surfacePanelColor(), 16)
	if ui2d.active {
		d2dDrawRoundedOutline(p, 16, 1, blendColor(theme.border, theme.accent2, .28))
	}
	drawText(hdc, "Уведомления", int(p.Left)+16, int(p.Top)+12, int(p.Right-p.Left)-32, 26, 16, 700, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	unread := notificationUnreadCount()
	countText := fmt.Sprintf("Непрочитанных: %d", unread)
	drawText(hdc, countText, int(p.Left)+150, int(p.Top)+13, int(p.Right-p.Left)-166, 24, 9, 500, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)

	drawButton(hdc, app.notificationUnreadOnlyRect, map[bool]string{true: "Непрочитанные", false: "Все"}[app.notificationUnreadOnly], app.notificationUnreadOnly)
	drawOutlinedButton(hdc, app.notificationMarkReadRect, "Пометить всё прочитанным", theme.accent2)
	drawScenarioIconButton(hdc, app.notificationClearRect, scenarioIconNotificationClear)

	items := notificationItemsSnapshot(app.notificationUnreadOnly)
	visible := 0
	if ui2d.active && app.notificationListClip.Right > app.notificationListClip.Left {
		d2dPushClip(app.notificationListClip)
	}
	for slot, r := range app.notificationRows {
		idx := app.notificationRowIndices[slot]
		if idx < 0 || idx >= len(items) || r.Right <= r.Left || r.Bottom <= app.notificationListClip.Top || r.Top >= app.notificationListClip.Bottom {
			continue
		}
		visible++
		n := items[idx]
		base := surfaceButtonColor()
		if n.Unread {
			base = blendColor(base, notificationKindColor(n.Kind), .08)
		}
		h := hoverAmount(r)
		if h > 0 {
			base = blendColor(base, theme.accent2, .07*h)
		}
		roundFill(hdc, r, base, 10)
		if n.Unread {
			d2dFillEllipse(float32(r.Left+8), float32(r.Top+10), 3.5, 3.5, notificationKindColor(n.Kind))
		}
		titleX := int(r.Left) + 16
		drawText(hdc, n.Title, titleX, int(r.Top)+5, int(r.Right)-titleX-108, 17, 10, 650, theme.text, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		drawText(hdc, notificationTimeLabel(n.When), int(r.Right)-102, int(r.Top)+5, 60, 17, 8, 500, theme.muted, DT_RIGHT|DT_VCENTER|DT_SINGLELINE)
		drawText(hdc, n.Text, titleX, int(r.Top)+24, int(r.Right)-titleX-46, 17, 9, 450, theme.muted, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		if n.Unread {
			drawScenarioIconButton(hdc, app.notificationReadRects[slot], scenarioIconNotificationRead)
		}
	}
	if ui2d.active && app.notificationListClip.Right > app.notificationListClip.Left {
		d2dPopClip()
	}
	drawScrollBar(hdc, app.notificationScrollTrack, app.notificationScrollThumb)
	if visible == 0 {
		empty := "Уведомлений пока нет."
		if app.notificationUnreadOnly {
			empty = "Непрочитанных уведомлений нет."
		}
		drawText(hdc, empty, int(p.Left)+18, int(app.notificationUnreadOnlyRect.Bottom)+42, int(p.Right-p.Left)-36, 40, 11, 500, theme.muted, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
	if app.confirmClearNotifications {
		drawNotificationClearConfirmation(hdc, p)
	} else {
		drawScenarioTooltip(hdc, p)
	}
}

func drawNotificationClearConfirmation(hdc uintptr, panel RECT) {
	fill(hdc, panel, blendColor(theme.bg, rgb(0, 0, 0), .46))
	w := minInt(320, int(panel.Right-panel.Left)-32)
	h := 154
	x := int(panel.Left+panel.Right)/2 - w/2
	y := int(panel.Top+panel.Bottom)/2 - h/2
	app.notificationConfirmRect = RECT{int32(x), int32(y), int32(x + w), int32(y + h)}
	roundFill(hdc, app.notificationConfirmRect, surfacePanelColor(), 14)
	if ui2d.active {
		d2dDrawRoundedOutline(app.notificationConfirmRect, 14, 1, blendColor(theme.border, theme.danger, .35))
	}
	drawText(hdc, "Очистить уведомления?", x+16, y+16, w-32, 24, 16, 650, theme.text, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	drawText(hdc, "История уведомлений будет удалена без возможности восстановления.", x+20, y+46, w-40, 38, 10, 450, theme.muted, DT_CENTER|DT_VCENTER|DT_WORDBREAK)
	btnY := y + 102
	app.notificationConfirmNoRect = RECT{int32(x + 16), int32(btnY), int32(x + w/2 - 6), int32(btnY + 38)}
	app.notificationConfirmYesRect = RECT{int32(x + w/2 + 6), int32(btnY), int32(x + w - 16), int32(btnY + 38)}
	drawButton(hdc, app.notificationConfirmNoRect, "Отмена", false)
	drawOutlinedButton(hdc, app.notificationConfirmYesRect, "Очистить", theme.danger)
}

func handleNotificationCenterClick(x, y int32) bool {
	if app.confirmSystemMode != 0 || app.confirmClearHistory {
		return false
	}
	if pointIn(app.notificationBtnRect, x, y) {
		app.notificationPanelOpen = !app.notificationPanelOpen
		app.confirmClearNotifications = false
		app.taskMenuOpen, app.createTaskMenuOpen, app.resourceMenuOpen = false, false, false
		playUI(clickSound)
		layoutControls(app.hwnd)
		if app.notificationPanelOpen {
			hideNativeInputs()
		} else {
			updateInputVisibility()
		}
		invalidate(app.hwnd)
		return true
	}
	if !app.notificationPanelOpen {
		return false
	}
	if app.confirmClearNotifications {
		if pointIn(app.notificationConfirmYesRect, x, y) {
			clearNotifications()
			app.confirmClearNotifications = false
			app.notificationScrollPx, app.notificationScrollTarget = 0, 0
			playUI(successSound)
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return true
		}
		if pointIn(app.notificationConfirmNoRect, x, y) || !pointIn(app.notificationConfirmRect, x, y) {
			app.confirmClearNotifications = false
			playUI(clickSound)
			invalidate(app.hwnd)
		}
		return true
	}
	if beginScrollbarInteraction(x, y) {
		return true
	}
	if pointIn(app.notificationUnreadOnlyRect, x, y) {
		app.notificationUnreadOnly = !app.notificationUnreadOnly
		app.notificationScrollPx, app.notificationScrollTarget = 0, 0
		playUI(clickSound)
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
		return true
	}
	if pointIn(app.notificationMarkReadRect, x, y) {
		markAllNotificationsRead()
		playUI(successSound)
		return true
	}
	if pointIn(app.notificationClearRect, x, y) {
		app.confirmClearNotifications = true
		playUI(openSound)
		invalidate(app.hwnd)
		return true
	}
	items := notificationItemsSnapshot(app.notificationUnreadOnly)
	for slot, r := range app.notificationRows {
		idx := app.notificationRowIndices[slot]
		if idx < 0 || idx >= len(items) || r.Right <= r.Left || !pointIn(app.notificationListClip, x, y) {
			continue
		}
		n := items[idx]
		if n.Unread && pointIn(app.notificationReadRects[slot], x, y) {
			markNotificationRead(n.ID)
			playUI(successSound)
			layoutControls(app.hwnd)
			invalidate(app.hwnd)
			return true
		}
		if pointIn(r, x, y) {
			markNotificationRead(n.ID)
			app.notificationPanelOpen = false
			openNotificationTarget(n.Target)
			playUI(openSound)
			return true
		}
	}
	if pointIn(app.notificationPanelRect, x, y) {
		return true
	}
	app.notificationPanelOpen = false
	app.confirmClearNotifications = false
	layoutControls(app.hwnd)
	updateInputVisibility()
	invalidate(app.hwnd)
	return false
}

func openNotificationTarget(target int) {
	switch target {
	case notifTargetHistory, notifTargetSensors, notifTargetData:
		rememberCurrentTaskLocation()
		if isTaskSection(app.section) {
			syncFields()
		}
		app.section = 3
		if target == notifTargetHistory {
			app.settingsSubpage = 2
		} else {
			app.settingsSubpage = 4
		}
		startPageAnimation()
		updateInputVisibility()
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	case notifTargetResources:
		app.section = 18
		startPageAnimation()
		updateInputVisibility()
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	default:
		layoutControls(app.hwnd)
		invalidate(app.hwnd)
	}
}
