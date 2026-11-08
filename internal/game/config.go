// Package game holds the trainer rules: theory gate, coins, shop, modes.
package game

import (
	"errors"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

type Config struct {
	Coins struct {
		Easy          int `json:"easy"`
		Medium        int `json:"medium"`
		Hard          int `json:"hard"`
		LevelBonus    int `json:"levelBonus"`
		RepeatPercent int `json:"repeatPercent"`
	} `json:"coins"`
	Shop []ShopItem `json:"shop"`
}

type ShopItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Minutes int    `json:"minutes"`
	Price   int    `json:"price"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.UnmarshalStrict(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if p := cfg.Coins.RepeatPercent; p < 0 || p > 100 {
		return cfg, fmt.Errorf("%s: coins.repeatPercent must be 0..100, got %d", path, p)
	}
	seen := map[string]bool{}
	for _, it := range cfg.Shop {
		switch {
		case it.ID == "" || it.Title == "":
			return cfg, errors.New(path + ": shop item needs id and title")
		case seen[it.ID]:
			return cfg, fmt.Errorf("%s: duplicate shop item %q", path, it.ID)
		case it.Minutes <= 0 || it.Price <= 0:
			return cfg, fmt.Errorf("%s: shop item %q needs minutes > 0 and price > 0", path, it.ID)
		}
		seen[it.ID] = true
	}
	return cfg, nil
}
