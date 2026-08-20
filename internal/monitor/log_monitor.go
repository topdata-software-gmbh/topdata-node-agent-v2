package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/hpcloud/tail"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	criticalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "shopware_critical_errors_total",
		Help: "Total number of critical errors in Shopware logs",
	}, []string{"shop"})
)

func TailLog(shopName, logDir string) {
	var current *tail.Tail
	var lastDay string

	for {
		day := time.Now().Format("2006-01-02")
		if day != lastDay {
			if current != nil {
				current.Stop()
				current.Cleanup()
				current = nil
			}
			lastDay = day
			next, err := tail.TailFile(fmt.Sprintf("%s/prod-%s.log", logDir, day), tail.Config{
				Follow:    true,
				ReOpen:    true,
				MustExist: false,
				Poll:      true,
			})
			if err != nil {
				time.Sleep(10 * time.Second)
				continue
			}
			current = next
			go func(t *tail.Tail) {
				for line := range t.Lines {
					if strings.Contains(line.Text, ".CRITICAL:") || strings.Contains(line.Text, "[critical]") {
						criticalErrors.WithLabelValues(shopName).Inc()
					}
				}
			}(current)
		}
		time.Sleep(30 * time.Second)
	}
}
