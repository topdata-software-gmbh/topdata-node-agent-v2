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
		if entry.IsDir() {
			shopPath := filepath.Join(root, entry.Name())
			logDir := filepath.Join(shopPath, "var/log")
			if _, err := os.Stat(logDir); err == nil {
				shops = append(shops, Shop{
					Name:    entry.Name(),
					Path:    shopPath,
					LogPath: logDir,
				})
			}
		}
	}
	return shops, nil
}
