package monitor

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	diskUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "shopware_shop_disk_usage_bytes",
		Help: "Disk usage of the shop directory in bytes",
	}, []string{"shop"})
)

func UpdateDiskUsage(shopName, shopPath string) {
	out, err := exec.Command("du", "-sb", shopPath).Output()
	if err == nil {
		parts := strings.Fields(string(out))
		if len(parts) > 0 {
			bytes, _ := strconv.ParseFloat(parts[0], 64)
			diskUsage.WithLabelValues(shopName).Set(bytes)
		}
	}
}
