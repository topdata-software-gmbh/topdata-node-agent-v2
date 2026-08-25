package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// resetCriticalStore clears the global buffer between tests. Tests must not
// run in parallel (shared global state).
func resetCriticalStore() {
	criticalMu.Lock()
	defer criticalMu.Unlock()
	criticalStore = map[string][]CriticalEntry{}
}

func TestRecordCriticalEvictsOldest(t *testing.T) {
	resetCriticalStore()
	for i := 0; i < criticalBufferSize+10; i++ {
		recordCritical("shop-a", fmt.Sprintf("line-%d", i))
	}
	got := snapshotCritical("shop-a", 0)
	if len(got["shop-a"]) != criticalBufferSize {
		t.Fatalf("buffer size = %d, want %d", len(got["shop-a"]), criticalBufferSize)
	}
	newest := got["shop-a"][0]
	oldest := got["shop-a"][len(got["shop-a"])-1]
	if newest.Message != fmt.Sprintf("line-%d", criticalBufferSize+9) {
		t.Errorf("newest = %q, want line-%d", newest.Message, criticalBufferSize+9)
	}
	if oldest.Message != "line-10" {
		t.Errorf("oldest kept = %q, want line-10", oldest.Message)
	}
}

func TestSnapshotCriticalNewestFirstAndLimit(t *testing.T) {
	resetCriticalStore()
	base := time.Now()
	criticalMu.Lock()
	for i := 0; i < 5; i++ {
		e := CriticalEntry{Timestamp: base.Add(time.Duration(i) * time.Second), Message: fmt.Sprintf("m%d", i)}
		criticalStore["shop-a"] = append(criticalStore["shop-a"], e)
	}
	criticalMu.Unlock()

	got := snapshotCritical("", 3)
	if got["shop-a"][0].Message != "m4" || got["shop-a"][2].Message != "m2" {
		t.Errorf("snapshot not newest-first with limit applied: %+v", got["shop-a"])
	}
	if len(got["shop-a"]) != 3 {
		t.Fatalf("len = %d, want 3", len(got["shop-a"]))
	}

	full := snapshotCritical("", 0)
	if len(full["shop-a"]) != 5 {
		t.Errorf("unlimited snapshot len = %d, want 5", len(full["shop-a"]))
	}
}

func TestSnapshotCriticalShopFilterIsolation(t *testing.T) {
	resetCriticalStore()
	recordCritical("shop-a", "a1")
	recordCritical("shop-b", "b1")

	onlyA := snapshotCritical("shop-a", 0)
	if _, ok := onlyA["shop-b"]; ok {
		t.Errorf("?shop= filter leaked other shops: %+v", onlyA)
	}
	if len(onlyA["shop-a"]) != 1 || onlyA["shop-a"][0].Message != "a1" {
		t.Errorf("wrong entries for shop-a: %+v", onlyA["shop-a"])
	}

	all := snapshotCritical("", 0)
	if len(all) != 2 {
		t.Errorf("unfiltered snapshot has %d shops, want 2", len(all))
	}

	unknown := snapshotCritical("does-not-exist", 0)
	if len(unknown) != 0 {
		t.Errorf("unknown shop must yield empty result, got %+v", unknown)
	}
}

func TestRemoveShopCritical(t *testing.T) {
	resetCriticalStore()
	recordCritical("shop-a", "a1")
	removeShopCritical("shop-a")
	got := snapshotCritical("shop-a", 0)
	if len(got) != 0 {
		t.Errorf("removed shop still present: %+v", got)
	}
}

func TestCriticalErrorsHandlerFormats(t *testing.T) {
	resetCriticalStore()
	started := time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC)
	recordCritical("shop-a", "boom .CRITICAL: x|y")

	h := CriticalErrorsHandler(started)

	t.Run("json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/critical-errors?shop=shop-a", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var p criticalPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		if p.AgentStartedAt != started.Format(time.RFC3339) {
			t.Errorf("agent_started_at = %q", p.AgentStartedAt)
		}
		if p.Total != 1 || len(p.Shops["shop-a"]) != 1 || p.Shops["shop-a"][0].Message != "boom .CRITICAL: x|y" {
			t.Errorf("payload mismatch: %+v", p)
		}
	})

	t.Run("text", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/critical-errors?format=text", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		for _, want := range []string{"shop-a (1 entries)", "MESSAGE", "boom .CRITICAL: x|y"} {
			if !strings.Contains(body, want) {
				t.Errorf("text output missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("markdown escapes pipes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/critical-errors?format=markdown", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		for _, want := range []string{"## shop-a (1 entries)", `x\|y`} {
			if !strings.Contains(body, want) {
				t.Errorf("markdown output missing %q:\n%s", want, body)
			}
		}
	})

	t.Run("empty state", func(t *testing.T) {
		resetCriticalStore()
		req := httptest.NewRequest(http.MethodGet, "/critical-errors?shop=nobody&format=text", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if body := rec.Body.String(); !strings.Contains(body, "no critical errors recorded since agent start") {
			t.Errorf("missing empty-state text:\n%s", body)
		}
	})

	t.Run("limit clamp", func(t *testing.T) {
		resetCriticalStore()
		for i := 0; i < criticalBufferSize+20; i++ {
			recordCritical("shop-a", fmt.Sprintf("m%d", i))
		}
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/critical-errors?limit=%d&shop=shop-a", criticalBufferSize+50), nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var p criticalPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		if len(p.Shops["shop-a"]) != criticalBufferSize {
			t.Errorf("clamped len = %d, want %d", len(p.Shops["shop-a"]), criticalBufferSize)
		}
	})
}
