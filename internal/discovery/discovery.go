package discovery

import (
	"os"
	"path/filepath"
)

type Shop struct {
	Name    string
	Path    string
	LogPath string
}

func FindShops(root string) ([]Shop, error) {
	var shops []Shop
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		shopPath := filepath.Join(root, entry.Name())
		logDir := findLogDir(shopPath)
		if logDir != "" {
			shops = append(shops, Shop{
				Name:    entry.Name(),
				Path:    shopPath,
				LogPath: logDir,
			})
		}
	}
	return shops, nil
}

// findLogDir returns the log directory for a shop. Shops use the Docker clone
// layout (<shop>/vol/www/var/log).
func findLogDir(shopPath string) string {
	dir := filepath.Join(shopPath, "vol/www/var/log")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}
