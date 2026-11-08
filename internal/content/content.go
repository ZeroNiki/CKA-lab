// Package content loads levels, theory questions and practice tasks from YAML files.
package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"sigs.k8s.io/yaml"

	"cka-lab/internal/check"
)

type Level struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Domain    string     `json:"domain"`
	Order     int        `json:"order"`
	Questions []Question `json:"-"`
	Tasks     []Task     `json:"-"`
}

type Question struct {
	ID          string   `json:"id"`
	Question    string   `json:"question"`
	Options     []string `json:"options"`
	Answer      int      `json:"answer"`
	Explanation string   `json:"explanation"`
}

type Task struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	Difficulty       string       `json:"difficulty"`
	NS               string       `json:"ns"`
	RequiresSnapshot bool         `json:"requiresSnapshot"`
	Statement        string       `json:"statement"`
	Setup            Setup        `json:"setup"`
	Checks           []check.Spec `json:"checks"`
	Docs             string       `json:"docs"`
	Solution         string       `json:"solution"`
	SolutionScript   string       `json:"solutionScript"`
	SolutionNode     string       `json:"solutionNode,omitempty"` // where solutionScript runs, default "controlplane"
	Source           string       `json:"source,omitempty"`

	LevelID string `json:"-"` // set by Load
	File    string `json:"-"` // path relative to the content root, set by Load
}

type Setup struct {
	Manifests []string       `json:"manifests,omitempty"`
	Commands  []SetupCommand `json:"commands,omitempty"`
}

type SetupCommand struct {
	Node string `json:"node,omitempty"` // default "controlplane"
	Run  string `json:"run"`
}

type Content struct {
	Levels       []Level
	TaskByID     map[string]*Task
	QuestionByID map[string]*Question
}

// ValidationError points at the file and field an author has to fix.
type ValidationError struct {
	File, Field, Msg string
}

func (e *ValidationError) Error() string { return e.File + ": " + e.Field + ": " + e.Msg }

const (
	QuestionsPerLevel = 10
	TasksPerLevel     = 7
	defaultNode       = "controlplane"
)

// Load reads root/levels/<dir>/{level.yaml,theory.yaml,tasks/*.yaml} and validates everything.
func Load(root string) (*Content, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "levels", "*", "level.yaml"))
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no levels found under %s", filepath.Join(root, "levels"))
	}
	sort.Strings(dirs)

	c := &Content{TaskByID: map[string]*Task{}, QuestionByID: map[string]*Question{}}
	seenLevel := map[string]bool{}
	for _, levelFile := range dirs {
		lvl, err := loadLevel(root, filepath.Dir(levelFile))
		if err != nil {
			return nil, err
		}
		if seenLevel[lvl.ID] {
			return nil, &ValidationError{rel(root, levelFile), "id", fmt.Sprintf("duplicate level id %q", lvl.ID)}
		}
		seenLevel[lvl.ID] = true
		c.Levels = append(c.Levels, *lvl)
	}
	sort.SliceStable(c.Levels, func(i, j int) bool { return c.Levels[i].Order < c.Levels[j].Order })

	// Index after sorting so the pointers stay valid.
	for li := range c.Levels {
		l := &c.Levels[li]
		for qi := range l.Questions {
			q := &l.Questions[qi]
			if _, dup := c.QuestionByID[q.ID]; dup {
				return nil, &ValidationError{rel(root, filepath.Join(root, "levels")), "id", fmt.Sprintf("duplicate question id %q", q.ID)}
			}
			c.QuestionByID[q.ID] = q
		}
		for ti := range l.Tasks {
			t := &l.Tasks[ti]
			if prev, dup := c.TaskByID[t.ID]; dup {
				return nil, &ValidationError{t.File, "id", fmt.Sprintf("duplicate task id %q (also in %s)", t.ID, prev.File)}
			}
			c.TaskByID[t.ID] = t
		}
	}
	return c, nil
}

func loadLevel(root, dir string) (*Level, error) {
	levelFile := filepath.Join(dir, "level.yaml")
	var lvl Level
	if err := readYAML(levelFile, &lvl); err != nil {
		return nil, &ValidationError{rel(root, levelFile), "-", err.Error()}
	}
	if lvl.ID == "" || lvl.Title == "" {
		return nil, &ValidationError{rel(root, levelFile), "id", "id and title are required"}
	}

	theoryFile := filepath.Join(dir, "theory.yaml")
	if err := readYAML(theoryFile, &lvl.Questions); err != nil {
		return nil, &ValidationError{rel(root, theoryFile), "-", err.Error()}
	}
	if err := validateQuestions(rel(root, theoryFile), lvl.Questions); err != nil {
		return nil, err
	}

	taskFiles, err := filepath.Glob(filepath.Join(dir, "tasks", "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(taskFiles)
	seen := map[string]string{}
	for _, f := range taskFiles {
		var t Task
		if err := readYAML(f, &t); err != nil {
			return nil, &ValidationError{rel(root, f), "-", err.Error()}
		}
		t.LevelID = lvl.ID
		t.File = rel(root, f)
		if t.SolutionNode == "" {
			t.SolutionNode = defaultNode
		}
		for i := range t.Setup.Commands {
			if t.Setup.Commands[i].Node == "" {
				t.Setup.Commands[i].Node = defaultNode
			}
		}
		if prev, dup := seen[t.ID]; dup {
			return nil, &ValidationError{rel(root, f), "id", fmt.Sprintf("duplicate task id %q (also in %s)", t.ID, prev)}
		}
		seen[t.ID] = rel(root, f)
		if err := validateTask(rel(root, f), &t); err != nil {
			return nil, err
		}
		lvl.Tasks = append(lvl.Tasks, t)
	}
	if len(lvl.Tasks) != TasksPerLevel {
		return nil, &ValidationError{rel(root, filepath.Join(dir, "tasks")), "tasks",
			fmt.Sprintf("level %q has %d tasks, want %d", lvl.ID, len(lvl.Tasks), TasksPerLevel)}
	}
	return &lvl, nil
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.UnmarshalStrict(b, v); err != nil {
		return errors.New("invalid YAML: " + err.Error())
	}
	return nil
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
