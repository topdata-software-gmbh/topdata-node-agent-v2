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

		for _, shop := range shops {
			log.Printf("found shop %s (logs: %s)", shop.Name, shop.LogPath)
			go monitor.TailLog(shop.Name, shop.LogPath)
			go func(s discovery.Shop) {
				for {
					monitor.UpdateDiskUsage(s.Name, s.Path)
					time.Sleep(1 * time.Hour)
				}
			}(shop)
		}

		log.Printf("listening on %s", viper.GetString("listen.address"))
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
	viper.SetDefault("shops.root", "/srv/topdata-shops/prod-shops")
	viper.SetDefault("listen.address", ":9144")
	viper.SetEnvPrefix("TOPDATA_AGENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	serveCmd.Flags().String("shops-root", viper.GetString("shops.root"), "Directory containing shop folders")
	viper.BindPFlag("shops.root", serveCmd.Flags().Lookup("shops-root"))
	serveCmd.Flags().String("listen-address", viper.GetString("listen.address"), "Address to listen on")
	viper.BindPFlag("listen.address", serveCmd.Flags().Lookup("listen-address"))

	rootCmd.AddCommand(serveCmd)
}
