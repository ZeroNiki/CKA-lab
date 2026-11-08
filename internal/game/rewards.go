package game

// TaskReward is the coins for one solve. Showing the solution first earns nothing;
// repeat solves earn RepeatPercent so the same task cannot be farmed.
func TaskReward(cfg Config, difficulty string, firstSolve, revealed bool) int {
	if revealed {
		return 0
	}
	var base int
	switch difficulty {
	case "easy":
		base = cfg.Coins.Easy
	case "medium":
		base = cfg.Coins.Medium
	case "hard":
		base = cfg.Coins.Hard
	}
	if !firstSolve {
		return base * cfg.Coins.RepeatPercent / 100
	}
	return base
}
