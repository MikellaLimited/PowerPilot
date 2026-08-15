//go:build windows

package main

import (
	"testing"
	"time"
)

func recurringTaskAt(hhmm string) SavedTask {
	return SavedTask{
		Mode: 4,
		Recurrence: RecurrenceSpec{
			Enabled:  true,
			Kind:     0,
			TimeHHMM: hhmm,
		},
	}
}

func TestSavedRecurrenceDue(t *testing.T) {
	now := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.Local)
	task := recurringTaskAt("09:30")

	key, due := savedRecurrenceDue(task, now)
	if !due || key != "2026-08-15 09:30" {
		t.Fatalf("expected task to be due, got key=%q due=%v", key, due)
	}

	task.LastRunKey = key
	if _, due := savedRecurrenceDue(task, now); due {
		t.Fatal("task must not run twice in the same minute")
	}
}

func TestPausedSavedRecurrenceIsNotDue(t *testing.T) {
	now := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.Local)
	task := recurringTaskAt("09:30")
	task.Paused = true

	key, due := savedRecurrenceDue(task, now)
	if due || key != "" {
		t.Fatalf("paused task must not be due, got key=%q due=%v", key, due)
	}
}

func TestPausedTaskDoesNotArmWakeTimer(t *testing.T) {
	oldSettings := app.settings
	defer func() { app.settings = oldSettings }()

	now := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.Local)
	task := recurringTaskAt("09:30")
	task.Name = "Paused task"
	task.Paused = true
	app.settings.SavedTasks = []SavedTask{task}
	app.settings.WakeLeadMinutes = 5

	if target, name, ok := nextScheduledWake(now); ok {
		t.Fatalf("paused task armed wake timer: target=%v name=%q", target, name)
	}
}

func TestCompoundConditionValuesSupportNestedGroups(t *testing.T) {
	conds := []AutomationCondition{
		{ID: "outer", Type: condGroup, Enabled: true},
		{ID: "a", Type: condCPU, Enabled: true, GroupID: "outer"},
		{ID: "inner", Type: condGroup, Logic: logicAND, Enabled: true, GroupID: "outer"},
		{ID: "b", Type: condGPU, Enabled: true, GroupID: "inner"},
		{ID: "c", Type: condDisk, Logic: logicOR, Enabled: true, GroupID: "inner"},
	}

	got, found := evalCompoundConditionValues(conds, map[string]bool{"a": true, "b": false, "c": true}, "", map[string]bool{})
	if !found || !got {
		t.Fatalf("expected nested expression true, got found=%v value=%v", found, got)
	}

	got, found = evalCompoundConditionValues(conds, map[string]bool{"a": false, "b": false, "c": true}, "", map[string]bool{})
	if !found || got {
		t.Fatalf("expected outer AND group false, got found=%v value=%v", found, got)
	}
}

func TestEmptyCompoundConditionIsFalse(t *testing.T) {
	conds := []AutomationCondition{{ID: "empty", Type: condGroup, Enabled: true}}
	got, found := evalCompoundConditionValues(conds, map[string]bool{}, "", map[string]bool{})
	if !found || got {
		t.Fatalf("empty compound condition must be a present false node, got found=%v value=%v", found, got)
	}
}

func TestRelativeGraphTick(t *testing.T) {
	if got := formatRelativeGraphTick(0); got != "0" {
		t.Fatalf("current sample label = %q", got)
	}
	if got := formatRelativeGraphTick(90 * time.Second); got != "−1м 30с" {
		t.Fatalf("relative label = %q", got)
	}
}

func TestLegacyParenthesesMigrateToCompoundGroup(t *testing.T) {
	legacy := []AutomationCondition{
		{ID: "a", Type: condCPU, Logic: logicAND, Enabled: true, OpenGroups: 1},
		{ID: "b", Type: condGPU, Logic: logicOR, Enabled: true, CloseGroups: 1},
	}
	migrated := migrateLegacyConditionGroups(legacy)
	if len(migrated) != 3 || migrated[0].Type != condGroup {
		t.Fatalf("expected group plus two leaves, got %#v", migrated)
	}
	groupID := migrated[0].ID
	if migrated[1].GroupID != groupID || migrated[2].GroupID != groupID {
		t.Fatalf("legacy leaves were not assigned to the new group: %#v", migrated)
	}
	got, found := evalCompoundConditionValues(migrated, map[string]bool{"a": false, "b": true}, "", map[string]bool{})
	if !found || !got {
		t.Fatalf("migrated OR group changed semantics: found=%v value=%v", found, got)
	}
}
