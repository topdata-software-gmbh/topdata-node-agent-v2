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
		shops, err := discovery.FindShops(viper.GetString("shops.root"))
		if err != nil {
			log.Fatalf("discovery failed: %v", err)
		}

		for _, shop := range shops {
			go monitor.TailLog(shop.Name, shop.LogPath)
			go func(s discovery.Shop) {
				for {
					monitor.UpdateDiskUsage(s.Name, s.Path)
					time.Sleep(1 * time.Hour)
				}
			}(shop)
		}

		http.Handle("/metrics", authMiddleware(promhttp.Handler()))
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
	viper.SetDefault("auth.username", "admin")
	viper.SetDefault("auth.password", "fete")
	viper.SetDefault("shops.root", "/srv/topdata-shops")
	viper.SetDefault("listen.address", ":9144")
	viper.SetEnvPrefix("TOPDATA_AGENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	rootCmd.AddCommand(serveCmd)
}
