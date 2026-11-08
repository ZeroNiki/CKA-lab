package check

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		name    string
		op      string
		actual  []string
		found   bool
		want    string
		pass    bool
		wantErr bool
	}{
		{"eq single", "eq", []string{"3"}, true, "3", true, false},
		{"eq all values", "eq", []string{"3", "3"}, true, "3", true, false},
		{"eq one differs", "eq", []string{"3", "2"}, true, "3", false, false},
		{"eq not found", "eq", nil, false, "3", false, false},
		{"ne", "ne", []string{"2"}, true, "3", true, false},
		{"ne equal", "ne", []string{"3"}, true, "3", false, false},
		{"ne not found", "ne", nil, false, "true", true, false},
		{"contains", "contains", []string{"nginx:1.27.3"}, true, "1.27", true, false},
		{"contains missing", "contains", []string{"nginx:1.26"}, true, "1.27", false, false},
		{"matches", "matches", []string{"web-7d9f"}, true, `^web-[a-z0-9]+$`, true, false},
		{"matches no", "matches", []string{"api-1"}, true, `^web-`, false, false},
		{"gte numeric", "gte", []string{"10"}, true, "9", true, false},
		{"gte less", "gte", []string{"8"}, true, "9", false, false},
		{"lte", "lte", []string{"9"}, true, "10", true, false},
		{"gte not a number", "gte", []string{"abc"}, true, "9", false, true},
		{"exists found", "exists", []string{"x"}, true, "", true, false},
		{"exists missing", "exists", nil, false, "", false, false},
		{"notExists missing", "notExists", nil, false, "", true, false},
		{"notExists found", "notExists", []string{"x"}, true, "", false, false},
		{"unknown op", "approx", []string{"1"}, true, "1", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass, err := Compare(tt.op, tt.actual, tt.found, tt.want)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if pass != tt.pass {
				t.Fatalf("pass = %v, want %v", pass, tt.pass)
			}
		})
	}
}
