package content

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

var difficulties = map[string]bool{"easy": true, "medium": true, "hard": true}

func validateQuestions(file string, qs []Question) error {
	if len(qs) != QuestionsPerLevel {
		return &ValidationError{file, "questions", fmt.Sprintf("got %d questions, want %d", len(qs), QuestionsPerLevel)}
	}
	seen := map[string]bool{}
	for i, q := range qs {
		where := fmt.Sprintf("question %d (%s)", i+1, q.ID)
		switch {
		case q.ID == "":
			return &ValidationError{file, "id", where + ": id is required"}
		case seen[q.ID]:
			return &ValidationError{file, "id", where + ": duplicate id"}
		case strings.TrimSpace(q.Question) == "":
			return &ValidationError{file, "question", where + ": text is required"}
		case len(q.Options) < 2:
			return &ValidationError{file, "options", where + ": need at least 2 options"}
		case q.Answer < 0 || q.Answer >= len(q.Options):
			return &ValidationError{file, "answer", fmt.Sprintf("%s: answer %d is out of range 0..%d", where, q.Answer, len(q.Options)-1)}
		case strings.TrimSpace(q.Explanation) == "":
			return &ValidationError{file, "explanation", where + ": explanation is required"}
		}
		seen[q.ID] = true
	}
	return nil
}

func validateTask(file string, t *Task) error {
	switch {
	case t.ID == "":
		return &ValidationError{file, "id", "id is required"}
	case strings.TrimSpace(t.Title) == "":
		return &ValidationError{file, "title", "title is required"}
	case !difficulties[t.Difficulty]:
		return &ValidationError{file, "difficulty", fmt.Sprintf("%q is not one of easy, medium, hard", t.Difficulty)}
	case !strings.HasPrefix(t.NS, "lab-"):
		// The trainer deletes and recreates this namespace, so it must never be a real one.
		return &ValidationError{file, "ns", fmt.Sprintf("%q must start with \"lab-\"", t.NS)}
	case strings.TrimSpace(t.Statement) == "":
		return &ValidationError{file, "statement", "statement is required"}
	case strings.TrimSpace(t.SolutionScript) == "":
		return &ValidationError{file, "solutionScript", "solutionScript is required (used by ./lab selftest)"}
	case len(t.Checks) == 0:
		return &ValidationError{file, "checks", "at least one check is required"}
	}
	for i, c := range t.Checks {
		if err := c.Validate(); err != nil {
			return &ValidationError{file, fmt.Sprintf("checks[%d]", i), err.Error()}
		}
	}
	for i, m := range t.Setup.Manifests {
		if err := validateManifest(m); err != nil {
			return &ValidationError{file, fmt.Sprintf("setup.manifests[%d]", i), err.Error()}
		}
	}
	for i, c := range t.Setup.Commands {
		if strings.TrimSpace(c.Run) == "" {
			return &ValidationError{file, fmt.Sprintf("setup.commands[%d]", i), "run is required"}
		}
	}
	return nil
}

// validateManifest checks every YAML document has apiVersion and kind.
func validateManifest(m string) error {
	for i, doc := range SplitDocuments(m) {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return fmt.Errorf("document %d: %v", i+1, err)
		}
		if obj["apiVersion"] == nil || obj["kind"] == nil {
			return fmt.Errorf("document %d: apiVersion and kind are required", i+1)
		}
	}
	return nil
}

// SplitDocuments splits a multi-document YAML string and drops empty documents.
func SplitDocuments(m string) []string {
	var docs []string
	for _, d := range strings.Split("\n"+m, "\n---") {
		if strings.TrimSpace(d) != "" {
			docs = append(docs, d)
		}
	}
	return docs
}
