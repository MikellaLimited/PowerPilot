//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

type AppResourceStat struct {
	Name          string  `json:"name"`
	CPU           float64 `json:"cpu"`
	GPU           float64 `json:"gpu"`
	RAMMB         float64 `json:"ram_mb"`
	ReadKBps      float64 `json:"read_kbps"`
	WriteKBps     float64 `json:"write_kbps"`
	NetworkKBps   float64 `json:"network_kbps,omitempty"`
	TrafficKB     float64 `json:"traffic_kb,omitempty"`
	ActiveSeconds float64 `json:"active_seconds,omitempty"`
}

type ResourceStatSample struct {
	At           time.Time         `json:"at"`
	CPU          float64           `json:"cpu"`
	GPU          float64           `json:"gpu"`
	RAM          float64           `json:"ram"`
	Disk         float64           `json:"disk"`
	NetworkKBps  float64           `json:"network_kbps"`
	AppTrafficKB float64           `json:"app_traffic_kb,omitempty"`
	Apps         []AppResourceStat `json:"apps,omitempty"`
}

type AppLongTermStat struct {
	Name          string  `json:"name"`
	TrafficKB     float64 `json:"traffic_kb"`
	ActiveSeconds float64 `json:"active_seconds"`
}

type AppLongTermDay struct {
	Day  string            `json:"day"`
	Apps []AppLongTermStat `json:"apps"`
}

var resourceAppHistory = struct {
	sync.RWMutex
	Days      []AppLongTermDay
	LastFlush time.Time
	Loaded    bool
}{}

var resourceStats = struct {
	sync.RWMutex
	Samples    []ResourceStatSample
	LastBucket time.Time
	LastFlush  time.Time
	Loaded     bool
}{}

func resourceStatsPath() string { return filepath.Join(settingsDir(), "resource_stats.json") }
func resourceAppHistoryPath() string {
	return filepath.Join(settingsDir(), "resource_app_history.json")
}

func initResourceStats() {
	resourceStats.Lock()
	if resourceStats.Loaded {
		resourceStats.Unlock()
		initResourceAppHistory()
		return
	}
	resourceStats.Loaded = true
	var data []ResourceStatSample
	if b, err := os.ReadFile(resourceStatsPath()); err == nil && json.Unmarshal(b, &data) == nil {
		cutoff := time.Now().Add(-30 * 24 * time.Hour)
		for _, v := range data {
			if v.At.After(cutoff) {
				resourceStats.Samples = append(resourceStats.Samples, v)
			}
		}
	}
	resourceStats.Unlock()
	initResourceAppHistory()
}

func initResourceAppHistory() {
	resourceAppHistory.Lock()
	if resourceAppHistory.Loaded {
		resourceAppHistory.Unlock()
		return
	}
	resourceAppHistory.Loaded = true
	var days []AppLongTermDay
	if b, err := os.ReadFile(resourceAppHistoryPath()); err == nil && json.Unmarshal(b, &days) == nil {
		resourceAppHistory.Days = days
		resourceAppHistory.Unlock()
		return
	}
	resourceAppHistory.Unlock()

	// One-time migration: seed the permanent traffic/activity archive with whatever
	// detailed 30-day samples already exist. Future samples are stored before the
	// top-process truncation, so the permanent archive is complete for all apps.
	resourceStats.RLock()
	old := append([]ResourceStatSample(nil), resourceStats.Samples...)
	resourceStats.RUnlock()
	if len(old) == 0 {
		return
	}
	for _, sample := range old {
		recordResourceAppHistory(sample.At, sample.Apps, false)
	}
	flushResourceAppHistory(true)
}

func recordResourceStats(now time.Time, snap MetricSnapshot, procs []ProcessMetric) {
	resourceStats.Lock()
	if !resourceStats.LastBucket.IsZero() && now.Sub(resourceStats.LastBucket) < time.Minute {
		resourceStats.Unlock()
		return
	}
	resourceStats.LastBucket = now
	apps := make([]AppResourceStat, 0, len(procs))
	byName := map[string]*AppResourceStat{}
	fgPID := uint32(0)
	if fg, _, _ := pGetForegroundWindow040.Call(); fg != 0 {
		pGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&fgPID)))
	}
	for _, p := range procs {
		if p.System && !app.settings.ShowSystemProcesses {
			continue
		}
		key := strings.ToLower(p.Name)
		a := byName[key]
		if a == nil {
			a = &AppResourceStat{Name: p.Name}
			byName[key] = a
		}
		if p.CPUPercent >= 0 {
			a.CPU += p.CPUPercent
		}
		if p.GPUPercent >= 0 {
			a.GPU += p.GPUPercent
		}
		if p.RAMMB >= 0 {
			a.RAMMB += p.RAMMB
		}
		if p.ReadKBps >= 0 {
			a.ReadKBps += p.ReadKBps
		}
		if p.WriteKBps >= 0 {
			a.WriteKBps += p.WriteKBps
		}
		if p.OtherKBps >= 0 {
			a.NetworkKBps += p.OtherKBps
			a.TrafficKB += p.OtherKBps * 60.0
		}
		if fgPID != 0 && p.PID == fgPID {
			a.ActiveSeconds = 60
		}
	}
	totalAppTrafficKB := 0.0
	allApps := make([]AppResourceStat, 0, len(byName))
	for _, a := range byName {
		totalAppTrafficKB += a.TrafficKB
		if a.CPU > 100 {
			a.CPU = 100
		}
		if a.GPU > 100 {
			a.GPU = 100
		}
		apps = append(apps, *a)
		allApps = append(allApps, *a)
	}
	sort.Slice(apps, func(i, j int) bool {
		si := apps[i].CPU + apps[i].GPU + apps[i].RAMMB/512 + (apps[i].ReadKBps+apps[i].WriteKBps+apps[i].NetworkKBps)/2048
		sj := apps[j].CPU + apps[j].GPU + apps[j].RAMMB/512 + (apps[j].ReadKBps+apps[j].WriteKBps+apps[j].NetworkKBps)/2048
		return si > sj
	})
	if len(apps) > 12 {
		apps = apps[:12]
	}
	resourceStats.Samples = append(resourceStats.Samples, ResourceStatSample{At: now, CPU: snap.CPU, GPU: snap.GPU, RAM: snap.RAMPercent, Disk: snap.DiskPercent, NetworkKBps: snap.NetworkKBps, AppTrafficKB: totalAppTrafficKB, Apps: apps})
	cutoff := now.Add(-30 * 24 * time.Hour)
	k := 0
	for k < len(resourceStats.Samples) && resourceStats.Samples[k].At.Before(cutoff) {
		k++
	}
	if k > 0 {
		resourceStats.Samples = append([]ResourceStatSample(nil), resourceStats.Samples[k:]...)
	}
	needFlush := resourceStats.LastFlush.IsZero() || now.Sub(resourceStats.LastFlush) >= 5*time.Minute
	resourceStats.Unlock()

	// Traffic and foreground activity are accumulated for every process before the
	// detailed sample is truncated to the top 12. This archive is never age-pruned.
	recordResourceAppHistory(now, allApps, true)
	if needFlush {
		flushResourceStats(false)
	}
}

func recordResourceAppHistory(now time.Time, apps []AppResourceStat, allowFlush bool) {
	if len(apps) == 0 {
		return
	}
	dayKey := now.Local().Format("2006-01-02")
	resourceAppHistory.Lock()
	idx := -1
	for i := range resourceAppHistory.Days {
		if resourceAppHistory.Days[i].Day == dayKey {
			idx = i
			break
		}
	}
	if idx < 0 {
		resourceAppHistory.Days = append(resourceAppHistory.Days, AppLongTermDay{Day: dayKey})
		idx = len(resourceAppHistory.Days) - 1
	}
	day := &resourceAppHistory.Days[idx]
	byName := make(map[string]*AppLongTermStat, len(day.Apps))
	for i := range day.Apps {
		byName[strings.ToLower(day.Apps[i].Name)] = &day.Apps[i]
	}
	for _, a := range apps {
		if a.TrafficKB <= 0 && a.ActiveSeconds <= 0 {
			continue
		}
		key := strings.ToLower(a.Name)
		st := byName[key]
		if st == nil {
			day.Apps = append(day.Apps, AppLongTermStat{Name: a.Name})
			st = &day.Apps[len(day.Apps)-1]
			byName[key] = st
		}
		st.TrafficKB += a.TrafficKB
		st.ActiveSeconds += a.ActiveSeconds
	}
	needFlush := allowFlush && (resourceAppHistory.LastFlush.IsZero() || now.Sub(resourceAppHistory.LastFlush) >= 5*time.Minute)
	resourceAppHistory.Unlock()
	if needFlush {
		flushResourceAppHistory(false)
	}
}

func flushResourceAppHistory(force bool) {
	resourceAppHistory.Lock()
	if !force && !resourceAppHistory.LastFlush.IsZero() && time.Since(resourceAppHistory.LastFlush) < 5*time.Minute {
		resourceAppHistory.Unlock()
		return
	}
	b, err := json.Marshal(resourceAppHistory.Days)
	if err == nil {
		resourceAppHistory.LastFlush = time.Now()
	}
	resourceAppHistory.Unlock()
	if err == nil {
		_ = os.MkdirAll(settingsDir(), 0755)
		_ = os.WriteFile(resourceAppHistoryPath(), b, 0644)
	}
}

func resourceAppHistoryForPeriod(d time.Duration) ([]AppResourceStat, float64, time.Time, time.Time) {
	resourceAppHistory.RLock()
	days := append([]AppLongTermDay(nil), resourceAppHistory.Days...)
	resourceAppHistory.RUnlock()
	cutoff := time.Time{}
	if d > 0 {
		cutoff = time.Now().Add(-d)
	}
	agg := map[string]*AppResourceStat{}
	total := 0.0
	var start, end time.Time
	for _, day := range days {
		dt, err := time.ParseInLocation("2006-01-02", day.Day, time.Local)
		if err != nil {
			continue
		}
		if !cutoff.IsZero() && dt.Add(24*time.Hour).Before(cutoff) {
			continue
		}
		if start.IsZero() || dt.Before(start) {
			start = dt
		}
		if end.IsZero() || dt.After(end) {
			end = dt
		}
		for _, a := range day.Apps {
			key := strings.ToLower(a.Name)
			v := agg[key]
			if v == nil {
				v = &AppResourceStat{Name: a.Name}
				agg[key] = v
			}
			v.TrafficKB += a.TrafficKB
			v.ActiveSeconds += a.ActiveSeconds
			total += a.TrafficKB
		}
	}
	out := make([]AppResourceStat, 0, len(agg))
	for _, v := range agg {
		out = append(out, *v)
	}
	return out, total, start, end
}

func flushResourceStats(force bool) {
	resourceStats.Lock()
	if !force && !resourceStats.LastFlush.IsZero() && time.Since(resourceStats.LastFlush) < 5*time.Minute {
		resourceStats.Unlock()
		return
	}
	b, err := json.Marshal(resourceStats.Samples)
	if err == nil {
		resourceStats.LastFlush = time.Now()
	}
	resourceStats.Unlock()
	if err == nil {
		_ = os.MkdirAll(settingsDir(), 0755)
		_ = os.WriteFile(resourceStatsPath(), b, 0644)
	}
	flushResourceAppHistory(force)
}

func resourceStatsForPeriod(d time.Duration) []ResourceStatSample {
	resourceStats.RLock()
	defer resourceStats.RUnlock()
	cutoff := time.Now().Add(-d)
	out := make([]ResourceStatSample, 0)
	for _, s := range resourceStats.Samples {
		if !s.At.Before(cutoff) {
			out = append(out, s)
		}
	}
	return out
}

func reloadResourceStats() {
	resourceStats.Lock()
	resourceStats.Samples = nil
	resourceStats.LastBucket = time.Time{}
	resourceStats.LastFlush = time.Time{}
	resourceStats.Loaded = false
	resourceStats.Unlock()
	resourceAppHistory.Lock()
	resourceAppHistory.Days = nil
	resourceAppHistory.LastFlush = time.Time{}
	resourceAppHistory.Loaded = false
	resourceAppHistory.Unlock()
	initResourceStats()
}
