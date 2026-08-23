//go:build windows

package main

import (
	"math"
	"syscall"
	"testing"
	"unsafe"
)

func TestMIBIfRow2LayoutMatchesWindowsABI(t *testing.T) {
	var row mibIfRow2
	if size := unsafe.Sizeof(row); size != 1352 {
		t.Fatalf("unexpected MIB_IF_ROW2 size: %d", size)
	}
	if offset := unsafe.Offsetof(row.InOctets); offset != 1208 {
		t.Fatalf("unexpected InOctets offset: %d", offset)
	}
	var table mibIfTable2
	if offset := unsafe.Offsetof(table.Table); offset != 8 {
		t.Fatalf("unexpected MIB_IF_TABLE2 row offset: %d", offset)
	}
}

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
	physical := mibIfRow2{AdminStatus: 1, OperStatus: 1, MediaConnectState: 1, InterfaceFlags: 0x01, Type: 71}
	copy(physical.Description[:], syscall.StringToUTF16("Intel Wi-Fi 6"))
	if !usableNetworkInterface2(&physical) {
		t.Fatal("active physical interface was rejected")
	}
	virtual := physical
	virtual.InterfaceFlags = 0
	copy(virtual.Description[:], syscall.StringToUTF16("Hyper-V Virtual Ethernet Adapter"))
	if usableNetworkInterface2(&virtual) {
		t.Fatal("virtual adapter would duplicate physical network traffic")
	}
	disconnected := physical
	disconnected.MediaConnectState = 2
	if usableNetworkInterface2(&disconnected) {
		t.Fatal("disconnected interface was accepted")
	}
}
