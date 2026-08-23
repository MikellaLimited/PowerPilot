//go:build windows

package main

import (
	"math"
	"testing"
)

func TestNormalizeMetricSnapshotChecksAllResourceCards(t *testing.T) {
	s := MetricSnapshot{
		CPU: 120, GPU: math.NaN(), RAMPercent: 48, DiskPercent: -4,
		NetworkDownKBps: 12, NetworkUpKBps: math.Inf(1), BatteryPercent: 101,
		Disks: []DiskMetric{{Activity: 140}},
	}
	normalizeMetricSnapshot(&s)
	if s.CPU != 100 || s.GPU != -1 || s.RAMPercent != 48 || s.DiskPercent != -1 {
		t.Fatalf("percentage metrics were not normalized: %#v", s)
	}
	if s.NetworkDownKBps != 12 || s.NetworkUpKBps != 0 || s.NetworkKBps != 12 {
		t.Fatalf("network metrics were not normalized: %#v", s)
	}
	if s.BatteryPercent != 100 || len(s.Disks) != 1 || s.Disks[0].Activity != 100 {
		t.Fatalf("power or per-disk metrics were not normalized: %#v", s)
	}
}

func TestUsableNetworkInterfaceRejectsDuplicateSources(t *testing.T) {
	physical := mibIfRow{AdminStatus: 1, OperStatus: 5, Type: 71}
	copy(physical.Descr[:], []byte("Intel Wi-Fi 6"))
	physical.DescrLen = uint32(len("Intel Wi-Fi 6"))
	if !usableNetworkInterface(&physical) {
		t.Fatal("active physical interface was rejected")
	}
	virtual := physical
	copy(virtual.Descr[:], []byte("Hyper-V Virtual Ethernet Adapter"))
	virtual.DescrLen = uint32(len("Hyper-V Virtual Ethernet Adapter"))
	if usableNetworkInterface(&virtual) {
		t.Fatal("virtual adapter would duplicate physical network traffic")
	}
	disconnected := physical
	disconnected.OperStatus = 2
	if usableNetworkInterface(&disconnected) {
		t.Fatal("disconnected interface was accepted")
	}
}
