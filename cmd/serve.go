package cmd

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/topdata/node-agent/internal/discovery"
	"github.com/topdata/node-agent/internal/monitor"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the metrics exporter",
	Run: func(cmd *cobra.Command, args []string) {
		if !viper.IsSet("auth.username") || !viper.IsSet("auth.password") {
			log.Fatal("basic auth credentials not configured: set TOPDATA_AGENT_AUTH_USERNAME and TOPDATA_AGENT_AUTH_PASSWORD")
		}

		log.Printf("topdata-agent %s starting", version)
		log.Printf("shops root: %s", viper.GetString("shops.root"))

		shops, err := discovery.FindShops(viper.GetString("shops.root"))
		if err != nil {
			log.Fatalf("discovery failed: %v", err)
		}
		if len(shops) == 0 {
			log.Printf("no shops found under %s", viper.GetString("shops.root"))
		}

		monitor.SetShopsTotal(len(shops))

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
		scanner.Start(shops)

		for _, shop := range shops {
			log.Printf("found shop %s (logs: %s)", shop.Name, shop.LogPath)
			go monitor.TailLog(shop.Name, shop.LogPath)
		}

		log.Printf("listening on %s", viper.GetString("listen.address"))
		http.Handle("/metrics", authMiddleware(promhttp.Handler()))
		http.Handle("/disk-eaters", authMiddleware(scanner.Handler()))
		log.Fatal(http.ListenAndServe(viper.GetString("listen.address"), nil))
	},
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
	viper.SetEnvPrefix("TOPDATA_AGENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	serveCmd.Flags().String("shops-root", viper.GetString("shops.root"), "Directory containing shop folders")
	viper.BindPFlag("shops.root", serveCmd.Flags().Lookup("shops-root"))
	serveCmd.Flags().String("listen-address", viper.GetString("listen.address"), "Address to listen on")
	viper.BindPFlag("listen.address", serveCmd.Flags().Lookup("listen-address"))

	rootCmd.AddCommand(serveCmd)
}
