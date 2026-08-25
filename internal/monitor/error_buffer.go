package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// criticalBufferSize caps how many critical-error lines are kept per shop.
	// Fixed by design (brainstorm decision): no config knob, bounded memory.
	criticalBufferSize = 100
	// criticalDefaultLimit is the per-shop entry count returned by
	// /critical-errors when ?limit= is absent.
	criticalDefaultLimit = 20
)

// CriticalEntry is one captured critical Shopware log line. The message is
// stored verbatim (never truncated) so consumers — notably AI agents — get the
// full error text needed for debugging.
type CriticalEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// The critical-line store mirrors the package-level Prometheus metrics in
// log_monitor.go: a single mutex-guarded global shared by all per-shop tail
// goroutines (writers) and the /critical-errors handler (reader).
var (
	criticalMu    sync.Mutex
	criticalStore = map[string][]CriticalEntry{} // shop name -> oldest..newest
)

// recordCritical appends a critical line for a shop, evicting the oldest
// entry once the per-shop buffer exceeds criticalBufferSize.
func recordCritical(shop, line string) {
	entry := CriticalEntry{Timestamp: time.Now(), Message: line}
	criticalMu.Lock()
	defer criticalMu.Unlock()
	buf := append(criticalStore[shop], entry)
	if len(buf) > criticalBufferSize {
		buf = buf[len(buf)-criticalBufferSize:]
	}
	criticalStore[shop] = buf
}

// removeShopCritical drops all buffered entries for a removed shop so
// /critical-errors stops reporting it, mirroring the deletion of the shop's
// Prometheus series in RemoveShopLog.
func removeShopCritical(shop string) {
	criticalMu.Lock()
	defer criticalMu.Unlock()
	delete(criticalStore, shop)
}

// snapshotCritical returns a copy of the buffered entries, newest first,
// optionally scoped to one shop, at most limit entries per shop (limit <= 0
// means unlimited). Callers own the returned value; the lock is released
// before any rendering happens.
func snapshotCritical(shop string, limit int) map[string][]CriticalEntry {
	criticalMu.Lock()
	defer criticalMu.Unlock()

	out := make(map[string][]CriticalEntry, len(criticalStore))
	for name, buf := range criticalStore {
		if shop != "" && name != shop {
			continue
		}
		entries := make([]CriticalEntry, len(buf))
		copy(entries, buf)
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
		if limit > 0 && len(entries) > limit {
			entries = entries[:limit]
		}
		out[name] = entries
	}
	return out
}

// criticalPayload is the JSON document served by /critical-errors.
type criticalPayload struct {
	AgentStartedAt string                     `json:"agent_started_at"`
	Shops          map[string][]CriticalEntry `json:"shops"`
	Total          int                        `json:"total"`
}

// CriticalErrorsHandler serves the /critical-errors listing behind basic auth.
//
// Query parameters:
//   - ?shop=<name>  only that shop's entries (unknown shop -> empty list)
//   - ?limit=<n>    max entries per shop (default 20, capped at buffer size)
//
// Output format is chosen by `?format=json|text|markdown`; if absent it falls
// back to content negotiation via the Accept header (text/plain or
// text/markdown) and finally defaults to JSON — the same negotiation as
// /disk-eaters and /info.
func CriticalErrorsHandler(agentStartedAt time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shop := r.URL.Query().Get("shop")
		limit := criticalDefaultLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > criticalBufferSize {
			limit = criticalBufferSize
		}
		entries := snapshotCritical(shop, limit)

		format := r.URL.Query().Get("format")
		if format == "" {
			accept := r.Header.Get("Accept")
			switch {
			case strings.Contains(accept, "text/markdown"):
				format = "markdown"
			case strings.Contains(accept, "text/plain"):
				format = "text"
			default:
				format = "json"
			}
		}

		switch format {
		case "text":
			writeCriticalText(w, entries, agentStartedAt)
		case "markdown":
			writeCriticalMarkdown(w, entries, agentStartedAt)
		default:
			total := 0
			for _, list := range entries {
				total += len(list)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(criticalPayload{
				AgentStartedAt: agentStartedAt.Format(time.RFC3339),
				Shops:          entries,
				Total:          total,
			})
		}
	})
}

// sortedCriticalShops returns the shop names in a snapshot alphabetically for
// deterministic text/markdown output.
func sortedCriticalShops(entries map[string][]CriticalEntry) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writeCriticalText renders the plain-text listing: one labelled block per
// shop with TIMESTAMP/MESSAGE columns.
func writeCriticalText(w http.ResponseWriter, entries map[string][]CriticalEntry, startedAt time.Time) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if len(entries) == 0 {
		fmt.Fprintf(w, "no critical errors recorded since agent start (%s)\n", startedAt.Format(time.RFC3339))
		return
	}
	for _, shop := range sortedCriticalShops(entries) {
		fmt.Fprintf(w, "%s (%d entries)\n", shop, len(entries[shop]))
		fmt.Fprintf(w, "%-25s %s\n", "TIMESTAMP", "MESSAGE")
		for _, e := range entries[shop] {
			fmt.Fprintf(w, "%-25s %s\n", e.Timestamp.Format(time.RFC3339), e.Message)
		}
		fmt.Fprintln(w)
	}
}

// writeCriticalMarkdown renders the markdown listing: one heading + table per
// shop. Pipes inside messages are escaped to keep the table intact.
func writeCriticalMarkdown(w http.ResponseWriter, entries map[string][]CriticalEntry, startedAt time.Time) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	if len(entries) == 0 {
		fmt.Fprintf(w, "_no critical errors recorded since agent start (%s)_\n", startedAt.Format(time.RFC3339))
		return
	}
	for _, shop := range sortedCriticalShops(entries) {
		fmt.Fprintf(w, "## %s (%d entries)\n\n", shop, len(entries[shop]))
		fmt.Fprintf(w, "| Timestamp | Message |\n")
		fmt.Fprintf(w, "| --- | --- |\n")
		for _, e := range entries[shop] {
			msg := strings.ReplaceAll(e.Message, "|", "\\|")
			fmt.Fprintf(w, "| %s | %s |\n", e.Timestamp.Format(time.RFC3339), msg)
		}
		fmt.Fprintln(w)
	}
}
