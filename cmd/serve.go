package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/topdata/node-agent/internal/monitor"
)

var startTime = time.Now()

var agentInfo = struct {
	ListenAddress string
	ShopsRoot     string
}{}

// shopCount reports the number of currently monitored shops. It is set by the
// supervisor in Run so the live count is available to the /info handler.
var shopCount func() int = func() int { return 0 }

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the metrics exporter",
	Run: func(cmd *cobra.Command, args []string) {
		if !viper.IsSet("auth.username") || !viper.IsSet("auth.password") {
			log.Fatal("basic auth credentials not configured: set TOPDATA_AGENT_AUTH_USERNAME and TOPDATA_AGENT_AUTH_PASSWORD")
		}

		log.Printf("topdata-agent %s starting", version)
		log.Printf("shops root: %s", viper.GetString("shops.root"))

		discoveryInterval := viper.GetDuration("discovery.interval")
		if discoveryInterval <= 0 {
			discoveryInterval = 15 * time.Minute
		}

		agentInfo.ListenAddress = viper.GetString("listen.address")
		agentInfo.ShopsRoot = viper.GetString("shops.root")

		scanInterval := viper.GetDuration("disk.scan_interval")
		if scanInterval <= 0 {
			scanInterval = 6 * time.Hour
		}
		scanConcurrency := viper.GetInt("disk.scan_concurrency")
		if scanConcurrency <= 0 {
			scanConcurrency = 1
		}
		excludeList := viper.GetStringSlice("disk.exclude")
		if len(excludeList) == 0 {
			excludeList = []string{"var/cache"}
		}
		maxDepth := viper.GetInt("disk.growth_max_depth")
		if maxDepth <= 0 {
			maxDepth = 3
		}
		stateFile := viper.GetString("disk.state_file")
		if stateFile == "" {
			stateFile = "/var/lib/topdata-agent/disk-state.json"
		}

		scanner := monitor.NewDiskScanner(scanInterval, scanConcurrency, excludeList, maxDepth, stateFile)
		supervisor := monitor.NewShopSupervisor(viper.GetString("shops.root"), discoveryInterval, scanner)
		shopCount = supervisor.Count
		go supervisor.Run()

		log.Printf("listening on %s", viper.GetString("listen.address"))
		http.HandleFunc("/healthz", healthzHandler)
		http.Handle("/metrics", authMiddleware(promhttp.Handler()))
		http.Handle("/disk-eaters", authMiddleware(scanner.Handler()))
		http.Handle("/info", authMiddleware(http.HandlerFunc(infoHandler)))
		http.Handle("/critical-errors", authMiddleware(monitor.CriticalErrorsHandler(startTime)))
		log.Fatal(http.ListenAndServe(viper.GetString("listen.address"), nil))
	},
}

// healthzHandler is an unauthenticated liveness probe. It only confirms the
// process is up and listening; it does not depend on any subsystem being ready.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK\n"))
}

// infoHandler is an authenticated endpoint that reports basic agent info
// (version, uptime, config). Output format follows the same negotiation as
// /disk-eaters: ?format=json|text|markdown, falling back to the Accept header
// and finally JSON.
func infoHandler(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Version       string  `json:"version"`
		UptimeSeconds float64 `json:"uptime_seconds"`
		Uptime        string  `json:"uptime"`
		StartedAt     string  `json:"started_at"`
		ListenAddress string  `json:"listen_address"`
		ShopsRoot     string  `json:"shops_root"`
		ShopsTotal    int     `json:"shops_total"`
	}{
		Version:       version,
		UptimeSeconds: time.Since(startTime).Seconds(),
		Uptime:        humanDuration(time.Since(startTime)),
		StartedAt:     startTime.Format(time.RFC3339),
		ListenAddress: agentInfo.ListenAddress,
		ShopsRoot:     agentInfo.ShopsRoot,
		ShopsTotal:    shopCount(),
	}

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
		fmt.Fprintf(w, "%-16s %s\n", "version", data.Version)
		fmt.Fprintf(w, "%-16s %s\n", "uptime", data.Uptime)
		fmt.Fprintf(w, "%-16s %s\n", "started_at", data.StartedAt)
		fmt.Fprintf(w, "%-16s %s\n", "listen_address", data.ListenAddress)
		fmt.Fprintf(w, "%-16s %s\n", "shops_root", data.ShopsRoot)
		fmt.Fprintf(w, "%-16s %d\n", "shops_total", data.ShopsTotal)
	case "markdown":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprintf(w, "| %s | %s |\n", "Field", "Value")
		fmt.Fprintf(w, "| --- | --- |\n")
		fmt.Fprintf(w, "| %s | %s |\n", "version", data.Version)
		fmt.Fprintf(w, "| %s | %s |\n", "uptime", data.Uptime)
		fmt.Fprintf(w, "| %s | %s |\n", "started_at", data.StartedAt)
		fmt.Fprintf(w, "| %s | %s |\n", "listen_address", data.ListenAddress)
		fmt.Fprintf(w, "| %s | %s |\n", "shops_root", data.ShopsRoot)
		fmt.Fprintf(w, "| %s | %d |\n", "shops_total", data.ShopsTotal)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int(d / time.Second)
	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if h > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
	}
	if m > 0 || h > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%dm", m))
	}
	parts = append(parts, fmt.Sprintf("%ds", s))
	return strings.Join(parts, " ")
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != viper.GetString("auth.username") || pass != viper.GetString("auth.password") {
			w.Header().Set("WWW-Authenticate", `Basic realm="metrics"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func init() {
	viper.SetDefault("shops.root", "/srv/topdata-shops/prod-shops")
	viper.SetDefault("listen.address", ":9144")
	viper.SetDefault("disk.scan_interval", 6*time.Hour)
	viper.SetDefault("disk.scan_concurrency", 1)
	viper.SetDefault("disk.exclude", []string{"var/cache"})
	viper.SetDefault("disk.growth_max_depth", 3)
	viper.SetDefault("disk.state_file", "/var/lib/topdata-agent/disk-state.json")
	viper.SetDefault("discovery.interval", 15*time.Minute)
	viper.SetEnvPrefix("TOPDATA_AGENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	serveCmd.Flags().String("shops-root", viper.GetString("shops.root"), "Directory containing shop folders")
	viper.BindPFlag("shops.root", serveCmd.Flags().Lookup("shops-root"))
	serveCmd.Flags().String("listen-address", viper.GetString("listen.address"), "Address to listen on")
	viper.BindPFlag("listen.address", serveCmd.Flags().Lookup("listen-address"))

	rootCmd.AddCommand(serveCmd)
}
