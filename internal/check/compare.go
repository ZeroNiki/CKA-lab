package check

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Compare applies op to every value in actual; all must satisfy it.
// found=false means the path or object does not exist.
func Compare(op string, actual []string, found bool, want string) (bool, error) {
	switch op {
	case "exists":
		return found, nil
	case "notExists":
		return !found, nil
	}
	if !knownOps[op] || op == "count" {
		return false, fmt.Errorf("unknown op %q", op)
	}
	if !found || len(actual) == 0 {
		// "not equal to X" holds for a field that is not set at all.
		return op == "ne", nil
	}
	for _, v := range actual {
		ok, err := compareOne(op, v, want)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func compareOne(op, v, want string) (bool, error) {
	switch op {
	case "eq":
		return v == want, nil
	case "ne":
		return v != want, nil
	case "contains":
		return strings.Contains(v, want), nil
	case "matches":
		re, err := regexp.Compile(want)
		if err != nil {
			return false, err
		}
		return re.MatchString(v), nil
	case "gte", "lte":
		a, err1 := strconv.ParseFloat(v, 64)
		b, err2 := strconv.ParseFloat(want, 64)
		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("%s needs numbers, got %q and %q", op, v, want)
		}
		if op == "gte" {
			return a >= b, nil
		}
		return a <= b, nil
	}
	return false, fmt.Errorf("unknown op %q", op)
}
