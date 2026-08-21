package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/topdata/node-agent/internal/discovery"
)

var diskUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "topdata_agent_shopware_shop_disk_usage_bytes",
	Help: "Disk usage of the shop directory in bytes (excluding configured dirs)",
}, []string{"shop"})

type dirState struct {
	Size     int64 `json:"size"`
	ScanTime int64 `json:"scan_time"` // unix seconds of the scan that produced Size
}

// Grower is one directory ranked by disk-growth on the /disk-eaters endpoint.
type Grower struct {
	Shop               string  `json:"shop"`
	Path               string  `json:"path"`
	SizeBytes          int64   `json:"size_bytes"`
	GrowthBytesPerHour float64 `json:"growth_bytes_per_hour"`
	LastScanAgeSeconds float64 `json:"last_scan_age_seconds"`
}

// DiskScanner periodically walks each shop, updates the disk-usage gauge and
// maintains a per-directory growth index served by /disk-eaters.
type DiskScanner struct {
	interval    time.Duration
	concurrency int
	excludes    []string
	maxDepth    int
	stateFile   string

	sem chan struct{}

	mu      sync.Mutex
	state   map[string]map[string]dirState
	growers map[string][]Grower
}

func NewDiskScanner(interval time.Duration, concurrency int, excludes []string, maxDepth int, stateFile string) *DiskScanner {
	d := &DiskScanner{
		interval:    interval,
		concurrency: concurrency,
		excludes:    excludes,
		maxDepth:    maxDepth,
		stateFile:   stateFile,
		sem:         make(chan struct{}, maxDepthOrOne(concurrency)),
		state:       map[string]map[string]dirState{},
		growers:     map[string][]Grower{},
	}
	d.loadState()
	return d
}

func maxDepthOrOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func (d *DiskScanner) loadState() {
	data, err := os.ReadFile(d.stateFile)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &d.state)
}

func (d *DiskScanner) saveState() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(d.stateFile), 0o755); err != nil {
		log.Printf("disk scanner: cannot create state dir: %v", err)
		return
	}
	data, err := json.Marshal(d.state)
	if err != nil {
		return
	}
	if err := os.WriteFile(d.stateFile, data, 0o644); err != nil {
		log.Printf("disk scanner: cannot write state file: %v", err)
	}
}

func (d *DiskScanner) isExcluded(rel string) bool {
	for _, ex := range d.excludes {
		ex = strings.Trim(ex, "/")
		if ex == "" {
			continue
		}
		if rel == ex || strings.HasPrefix(rel, ex+"/") {
			return true
		}
	}
	return false
}

func (d *DiskScanner) scanShop(shop discovery.Shop) {
	now := time.Now()
	sizes := map[string]int64{}

	var walk func(abs, rel string, depth int) int64
	walk = func(abs, rel string, depth int) int64 {
		var sz int64
		ents, err := os.ReadDir(abs)
		if err != nil {
			return 0
		}
		for _, e := range ents {
			name := e.Name()
			childAbs := filepath.Join(abs, name)
			if e.IsDir() {
				childRel := name
				if rel != "" {
					childRel = rel + "/" + name
				}
				if d.isExcluded(childRel) {
					continue
				}
				childSz := walk(childAbs, childRel, depth+1)
				sz += childSz
				if depth+1 <= d.maxDepth {
					sizes[childRel] = childSz
				}
			} else if e.Type().IsRegular() {
				if fi, err := e.Info(); err == nil {
					sz += fi.Size()
				}
			}
		}
		return sz
	}

	total := walk(shop.Path, "", 0)
	sizes[""] = total
	diskUsage.WithLabelValues(shop.Name).Set(float64(total))
	d.recordGrowth(shop.Name, sizes, now)
}

func (d *DiskScanner) recordGrowth(shop string, sizes map[string]int64, now time.Time) {
	d.mu.Lock()
	shopState, ok := d.state[shop]
	if !ok {
		shopState = map[string]dirState{}
		d.state[shop] = shopState
	}

	var list []Grower
	for rel, sz := range sizes {
		if rel == "" {
			// root size is the gauge; skip it from the ranking list
			shopState[rel] = dirState{Size: sz, ScanTime: now.Unix()}
			continue
		}
		prev, had := shopState[rel]
		var growth, age float64
		if had && prev.ScanTime > 0 {
			elapsed := now.Sub(time.Unix(prev.ScanTime, 0))
			if h := elapsed.Hours(); h > 0 {
				growth = float64(sz-prev.Size) / h
				age = elapsed.Seconds()
			}
		}
		shopState[rel] = dirState{Size: sz, ScanTime: now.Unix()}
		list = append(list, Grower{
			Shop:               shop,
			Path:               rel,
			SizeBytes:          sz,
			GrowthBytesPerHour: growth,
			LastScanAgeSeconds: age,
		})
	}
	d.growers[shop] = list
	d.mu.Unlock()

	d.saveState()
}

// TopGrowers returns the ranked directories, optionally scoped to one shop.
func (d *DiskScanner) TopGrowers(shop string, top int, by string) []Grower {
	d.mu.Lock()
	var all []Grower
	if shop == "" {
		for _, l := range d.growers {
			all = append(all, l...)
		}
	} else {
		all = append(all, d.growers[shop]...)
	}
	d.mu.Unlock()

	switch by {
	case "size":
		sort.Slice(all, func(i, j int) bool { return all[i].SizeBytes > all[j].SizeBytes })
	default: // "rate" / "delta" — growth per hour
		sort.Slice(all, func(i, j int) bool { return all[i].GrowthBytesPerHour > all[j].GrowthBytesPerHour })
	}

	if top > 0 && len(all) > top {
		all = all[:top]
	}
	return all
}

// Start launches one goroutine per shop. The first scan runs promptly at boot
// (serialized through the semaphore so there is no disk storm), then each shop
// settles into its own phase (a fixed random offset within the interval) so the
// scans stay permanently desynchronized and never realign into a hourly spike.
func (d *DiskScanner) Start(shops []discovery.Shop) {
	for _, s := range shops {
		go func(sh discovery.Shop) {
			time.Sleep(time.Duration(rand.Int63n(int64(30 * time.Second)))) // small boot spread
			d.sem <- struct{}{}
			d.scanShop(sh)
			<-d.sem

			offset := time.Duration(rand.Int63n(int64(d.interval))) // fixed phase for this shop
			for {
				time.Sleep(d.interval + offset)
				d.sem <- struct{}{}
				d.scanShop(sh)
				<-d.sem
			}
		}(s)
	}
}

// humanBytes renders a byte count as a short human-readable string (e.g. 1.5G).
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(b)/float64(div), "KMGTPE"[exp])
}

// Handler serves the /disk-eaters ranking behind basic auth.
//
// Output format is chosen by `?format=json|text|markdown`; if absent it falls
// back to content negotiation via the Accept header (text/plain or
// text/markdown) and finally defaults to JSON. This lets the listing be viewed
// directly in a browser by appending `?format=text` or `?format=markdown`.
func (d *DiskScanner) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shop := r.URL.Query().Get("shop")
		top := 20
		if v := r.URL.Query().Get("top"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				top = n
			}
		}
		by := r.URL.Query().Get("by")
		if by == "" {
			by = "rate"
		}
		growers := d.TopGrowers(shop, top, by)

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
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprintf(w, "%-28s %-42s %12s %12s\n", "SHOP", "PATH", "SIZE", "GROWTH/h")
			for _, g := range growers {
				fmt.Fprintf(w, "%-28s %-42s %12s %12s\n", g.Shop, g.Path, humanBytes(g.SizeBytes), humanBytes(int64(g.GrowthBytesPerHour)))
			}
		case "markdown":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			fmt.Fprintf(w, "| %s | %s | %s | %s |\n", "Shop", "Path", "Size", "Growth/h")
			fmt.Fprintf(w, "| --- | --- | ---: | ---: |\n")
			for _, g := range growers {
				fmt.Fprintf(w, "| %s | %s | %s | %s |\n", g.Shop, g.Path, humanBytes(g.SizeBytes), humanBytes(int64(g.GrowthBytesPerHour)))
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(growers)
		}
	})
}
