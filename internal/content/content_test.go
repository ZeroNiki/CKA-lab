package content

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = "testdata/content"

// copyFixture copies the valid fixture into a temp dir so a test can break it.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	return dst
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_Valid(t *testing.T) {
	c, err := Load(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Levels) != 1 {
		t.Fatalf("levels = %d", len(c.Levels))
	}
	l := c.Levels[0]
	if l.ID != "demo" || len(l.Questions) != 10 || len(l.Tasks) != 7 {
		t.Fatalf("level = %s q=%d t=%d", l.ID, len(l.Questions), len(l.Tasks))
	}
	if l.Tasks[0].ID != "demo-t1" || l.Tasks[6].ID != "demo-t7" {
		t.Fatalf("tasks not ordered by file name: %s..%s", l.Tasks[0].ID, l.Tasks[6].ID)
	}
	task := c.TaskByID["demo-t7"]
	if task == nil || task.LevelID != "demo" || !task.RequiresSnapshot {
		t.Fatalf("TaskByID[demo-t7] = %+v", task)
	}
	if task.Checks[0].Target.Kind != "ConfigMap" || len(task.Setup.Manifests) != 1 {
		t.Fatalf("task decoded wrong: %+v", task)
	}
	if task.Setup.Commands[0].Node != "controlplane" {
		t.Fatalf("setup command node should default to controlplane, got %q", task.Setup.Commands[0].Node)
	}
	if q := c.QuestionByID["demo-q3"]; q == nil || q.Answer != 1 {
		t.Fatalf("QuestionByID[demo-q3] = %+v", q)
	}
}

func TestLoad_LevelsSortedByOrder(t *testing.T) {
	root := copyFixture(t)
	second := filepath.Join(root, "levels", "aaa")
	if err := os.CopyFS(second, os.DirFS(filepath.Join(fixture, "levels", "demo"))); err != nil {
		t.Fatal(err)
	}
	edit(t, filepath.Join(second, "level.yaml"), "id: demo", "id: second")
	edit(t, filepath.Join(second, "level.yaml"), "order: 1", "order: 2")
	// make ids unique
	for i := 1; i <= 7; i++ {
		p := filepath.Join(second, "tasks", "0"+string(rune('0'+i))+"-task.yaml")
		edit(t, p, "id: demo-t", "id: second-t")
	}
	b, _ := os.ReadFile(filepath.Join(second, "theory.yaml"))
	os.WriteFile(filepath.Join(second, "theory.yaml"), []byte(strings.ReplaceAll(string(b), "id: demo-q", "id: second-q")), 0o644)

	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Levels[0].ID != "demo" || c.Levels[1].ID != "second" {
		t.Fatalf("order = %s, %s", c.Levels[0].ID, c.Levels[1].ID)
	}
}

func TestLoad_Errors(t *testing.T) {
	level := filepath.Join("levels", "demo")
	task1 := filepath.Join(level, "tasks", "01-task.yaml")
	theory := filepath.Join(level, "theory.yaml")
	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string)
		wantFile  string
		wantField string
	}{
		{"nine questions", func(t *testing.T, r string) {
			b, _ := os.ReadFile(filepath.Join(r, theory))
			s := string(b)
			cut := strings.LastIndex(s, "- id: demo-q10")
			os.WriteFile(filepath.Join(r, theory), []byte(s[:cut]), 0o644)
		}, theory, "questions"},
		{"six tasks", func(t *testing.T, r string) {
			os.Remove(filepath.Join(r, level, "tasks", "07-task.yaml"))
		}, filepath.Join(level, "tasks"), "tasks"},
		{"duplicate task id", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "id: demo-t1", "id: demo-t2")
		}, filepath.Join(level, "tasks", "02-task.yaml"), "id"},
		{"answer out of range", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, theory), "  answer: 1", "  answer: 5")
		}, theory, "answer"},
		{"one option", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, theory), "    - Wrong A\n    - Right\n    - Wrong B\n  answer: 1", "    - Only\n  answer: 0")
		}, theory, "options"},
		{"bad difficulty", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "difficulty: medium", "difficulty: insane")
		}, task1, "difficulty"},
		{"empty statement", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "statement: |\n  Create a ConfigMap `cm1` in namespace `lab-demo-1`.", "statement: \"\"")
		}, task1, "statement"},
		{"empty solutionScript", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "solutionScript: |\n  kubectl -n lab-demo-1 create configmap cm1", "solutionScript: \"\"")
		}, task1, "solutionScript"},
		{"ns without lab- prefix", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "ns: lab-demo-1", "ns: default")
		}, task1, "ns"},
		{"no checks", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "checks:\n  - type: resource\n    description: ConfigMap cm1 exists\n    target: {apiVersion: v1, kind: ConfigMap, name: cm1}", "checks: []")
		}, task1, "checks"},
		{"invalid check", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "type: resource", "type: magic")
		}, task1, "checks[0]"},
		{"manifest not yaml", func(t *testing.T, r string) {
			edit(t, filepath.Join(r, task1), "      kind: ConfigMap", "      kind: [ConfigMap")
		}, task1, "setup.manifests[0]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			tt.mutate(t, root)
			_, err := Load(root)
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			if ve.File != tt.wantFile || ve.Field != tt.wantField {
				t.Fatalf("got %s / %s (%s), want %s / %s", ve.File, ve.Field, ve.Msg, tt.wantFile, tt.wantField)
			}
		})
	}
	t.Run("duplicate task id across levels", func(t *testing.T) {
		root := copyFixture(t)
		second := filepath.Join(root, "levels", "zzz")
		if err := os.CopyFS(second, os.DirFS(filepath.Join(fixture, "levels", "demo"))); err != nil {
			t.Fatal(err)
		}
		edit(t, filepath.Join(second, "level.yaml"), "id: demo", "id: other")
		b, _ := os.ReadFile(filepath.Join(second, "theory.yaml"))
		os.WriteFile(filepath.Join(second, "theory.yaml"), []byte(strings.ReplaceAll(string(b), "id: demo-q", "id: other-q")), 0o644)
		_, err := Load(root)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.File != filepath.Join("levels", "zzz", "tasks", "01-task.yaml") || ve.Field != "id" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("invalid check message", func(t *testing.T) {
		root := copyFixture(t)
		edit(t, filepath.Join(root, task1), "type: resource", "type: magic")
		_, err := Load(root)
		if err == nil || !strings.Contains(err.Error(), "unknown check type") {
			t.Fatalf("err = %v", err)
		}
	})
}
