package game

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Coins
	if c.Easy != 10 || c.Medium != 20 || c.Hard != 30 || c.LevelBonus != 50 || c.RepeatPercent != 30 {
		t.Fatalf("coins = %+v", c)
	}
	want := []ShopItem{
		{ID: "break-5", Title: "Break 5 min", Minutes: 5, Price: 30},
		{ID: "break-15", Title: "Break 15 min", Minutes: 15, Price: 80},
		{ID: "break-30", Title: "Break 30 min", Minutes: 30, Price: 150},
	}
	if len(cfg.Shop) != len(want) {
		t.Fatalf("shop = %+v", cfg.Shop)
	}
	for i := range want {
		if cfg.Shop[i] != want[i] {
			t.Fatalf("shop[%d] = %+v, want %+v", i, cfg.Shop[i], want[i])
		}
	}
}

func TestLoadConfig_Invalid(t *testing.T) {
	for name, src := range map[string]string{
		"zero price":     "coins: {hard: 30, repeatPercent: 30}\nshop: [{id: a, title: A, minutes: 5, price: 0}]",
		"zero minutes":   "coins: {hard: 30, repeatPercent: 30}\nshop: [{id: a, title: A, minutes: 0, price: 5}]",
		"duplicate id":   "coins: {hard: 30, repeatPercent: 30}\nshop: [{id: a, title: A, minutes: 5, price: 5}, {id: a, title: B, minutes: 5, price: 5}]",
		"repeat percent": "coins: {hard: 30, repeatPercent: 120}\nshop: []",
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.yaml")
			os.WriteFile(p, []byte(src), 0o644)
			if _, err := LoadConfig(p); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
