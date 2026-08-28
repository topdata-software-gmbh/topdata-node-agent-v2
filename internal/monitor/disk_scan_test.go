package monitor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestHasState(t *testing.T) {
	dir := t.TempDir()
	sf := filepath.Join(dir, "state.json")
	d := &DiskScanner{stateFile: sf, mu: sync.Mutex{}, state: map[string]map[string]dirState{}}
	if d.hasState("shop-a") {
		t.Fatal("expected no state for unknown shop")
	}
	d.state["shop-a"] = map[string]dirState{}
	if !d.hasState("shop-a") {
		t.Fatal("expected state after insert")
	}
}

func TestScanOptionsApplied(t *testing.T) {
	d := NewDiskScanner(time.Hour, 1, []string{"var/cache"}, 3, filepath.Join(t.TempDir(), "s.json"), ScanOptions{
		YieldEvery:        100,
		YieldSleep:        time.Millisecond,
		StateSaveInterval: 30 * time.Second,
		DeferFirstOnState: true,
	})
	if d.yieldEvery != 100 || d.yieldSleep != time.Millisecond || d.saveInterval != 30*time.Second || !d.deferFirst {
		t.Fatalf("scan options not applied: %+v", d)
	}
}

func TestScheduleSaveDebounced(t *testing.T) {
	sf := filepath.Join(t.TempDir(), "state.json")
	_ = os.Remove(sf)
	d := &DiskScanner{stateFile: sf, saveInterval: 50 * time.Millisecond, mu: sync.Mutex{}, state: map[string]map[string]dirState{}}
	d.state["s"] = map[string]dirState{"": {Size: 1, ScanTime: time.Now().Unix()}}

	// Rapid calls must not write synchronously.
	for i := 0; i < 5; i++ {
		d.scheduleSave()
	}
	if _, err := os.Stat(sf); !os.IsNotExist(err) {
		t.Fatal("expected debounced write to be delayed, file exists immediately")
	}

	// After the interval elapses, exactly one write should have happened.
	time.Sleep(80 * time.Millisecond)
	if _, err := os.Stat(sf); err != nil {
		t.Fatalf("expected state file written after interval: %v", err)
	}
}

func TestSubtractChildrenResidual(t *testing.T) {
	in := []Grower{
		{Path: "", SizeBytes: 100, GrowthBytesPerHour: 100},
		{Path: "a", SizeBytes: 90, GrowthBytesPerHour: 90},
		{Path: "a/b", SizeBytes: 10, GrowthBytesPerHour: 10},
		{Path: "c", SizeBytes: 5, GrowthBytesPerHour: 5},
	}
	out := subtractChildren(in)
	byPath := map[string]Grower{}
	for _, g := range out {
		byPath[g.Path] = g
	}
	// "" has biggest child "a" (90) -> residual 10.
	if byPath[""].SizeBytes != 10 {
		t.Errorf("root residual size = %d, want 10", byPath[""].SizeBytes)
	}
	// "a" has child "a/b" (10) -> residual 80.
	if byPath["a"].SizeBytes != 80 {
		t.Errorf("a residual size = %d, want 80", byPath["a"].SizeBytes)
	}
	// "c" has no child -> unchanged.
	if byPath["c"].SizeBytes != 5 {
		t.Errorf("c size = %d, want 5", byPath["c"].SizeBytes)
	}
}
