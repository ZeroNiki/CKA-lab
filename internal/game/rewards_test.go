package game

import "testing"

func testConfig() Config {
	var c Config
	c.Coins.Easy, c.Coins.Medium, c.Coins.Hard = 10, 20, 30
	c.Coins.LevelBonus, c.Coins.RepeatPercent = 50, 30
	c.Shop = []ShopItem{{ID: "break-5", Title: "Break 5 min", Minutes: 5, Price: 30}}
	return c
}

func TestTaskReward(t *testing.T) {
	cfg := testConfig()
	tests := []struct {
		difficulty      string
		first, revealed bool
		want            int
	}{
		{"hard", true, false, 30},
		{"medium", false, false, 6},
		{"hard", false, false, 9},
		{"easy", true, false, 10},
		{"hard", true, true, 0},
		{"hard", false, true, 0},
	}
	for _, tt := range tests {
		if got := TaskReward(cfg, tt.difficulty, tt.first, tt.revealed); got != tt.want {
			t.Errorf("TaskReward(%s, first=%v, revealed=%v) = %d, want %d", tt.difficulty, tt.first, tt.revealed, got, tt.want)
		}
	}
}
