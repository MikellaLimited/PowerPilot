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
