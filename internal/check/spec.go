// Package check runs declarative checks of a task against the cluster state.
package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Spec is one check from a task file. Which fields apply depends on Type.
type Spec struct {
	Type        string `json:"type"` // resource | podsReady | canI | command
	Description string `json:"description"`

	// resource
	Target *Target     `json:"target,omitempty"`
	Assert []Assertion `json:"assert,omitempty"`

	// podsReady
	Selector string `json:"selector,omitempty"`
	Min      int    `json:"min,omitempty"`

	// canI
	Subject     *Subject `json:"subject,omitempty"`
	Verb        string   `json:"verb,omitempty"`
	Resource    string   `json:"resource,omitempty"`
	Subresource string   `json:"subresource,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Allowed     *bool    `json:"allowed,omitempty"`

	// command
	Node     string     `json:"node,omitempty"`
	Run      string     `json:"run,omitempty"`
	ExitCode *int       `json:"exitCode,omitempty"`
	Stdout   *Assertion `json:"stdout,omitempty"`

	Retry   Duration `json:"retry,omitempty"`
	Timeout Duration `json:"timeout,omitempty"`
}

// Target selects Kubernetes objects by name or by label selector.
type Target struct {
	APIVersion    string `json:"apiVersion"`
	Kind          string `json:"kind"`
	Name          string `json:"name,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	LabelSelector string `json:"labelSelector,omitempty"`
}

// Assertion compares the values found at a JSONPath with Value using Op.
type Assertion struct {
	Path  string `json:"path,omitempty"`
	Op    string `json:"op"`
	Value string `json:"value,omitempty"`
}

// Subject is who a canI check asks about. ServiceAccount is "namespace:name".
type Subject struct {
	User           string `json:"user,omitempty"`
	Group          string `json:"group,omitempty"`
	ServiceAccount string `json:"serviceAccount,omitempty"`
}

// Result is the outcome of one check, shown to the user as is.
type Result struct {
	Description string `json:"description"`
	Pass        bool   `json:"pass"`
	Actual      string `json:"actual"`
}

// Duration reads "60s"-style strings from YAML/JSON.
type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"30s\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

var knownOps = map[string]bool{
	"exists": true, "notExists": true, "eq": true, "ne": true, "contains": true,
	"matches": true, "gte": true, "lte": true, "count": true,
}

// Validate reports the first problem that would make the check impossible to run.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Description) == "" {
		return errors.New("description is required")
	}
	switch s.Type {
	case "resource":
		return s.validateResource()
	case "podsReady":
		if s.Selector == "" {
			return errors.New("podsReady: selector is required")
		}
	case "canI":
		return s.validateCanI()
	case "command":
		if strings.TrimSpace(s.Run) == "" {
			return errors.New("command: run is required")
		}
		if s.Stdout != nil {
			return validateAssertion(*s.Stdout, false)
		}
	default:
		return fmt.Errorf("unknown check type %q", s.Type)
	}
	return nil
}

func (s Spec) validateResource() error {
	if s.Target == nil {
		return errors.New("resource: target is required")
	}
	if s.Target.APIVersion == "" || s.Target.Kind == "" {
		return errors.New("resource: target.apiVersion and target.kind are required")
	}
	if s.Target.Name != "" && s.Target.LabelSelector != "" {
		return errors.New("resource: use target.name or target.labelSelector, not both")
	}
	for _, a := range s.Assert {
		if err := validateAssertion(a, true); err != nil {
			return err
		}
	}
	return nil
}

func validateAssertion(a Assertion, allowCount bool) error {
	if !knownOps[a.Op] {
		return fmt.Errorf("unknown op %q", a.Op)
	}
	if a.Op == "count" {
		if !allowCount {
			return errors.New("op count is only valid for resource checks")
		}
		if a.Path != "" {
			return errors.New("op count counts objects and takes no path")
		}
	}
	if a.Op == "matches" {
		if _, err := regexp.Compile(a.Value); err != nil {
			return fmt.Errorf("invalid regex %q: %w", a.Value, err)
		}
	}
	return nil
}

func (s Spec) validateCanI() error {
	if s.Verb == "" || s.Resource == "" {
		return errors.New("canI: verb and resource are required")
	}
	if s.Subject == nil || (s.Subject.User == "" && s.Subject.Group == "" && s.Subject.ServiceAccount == "") {
		return errors.New("canI: subject (user, group or serviceAccount) is required")
	}
	if sa := s.Subject.ServiceAccount; sa != "" {
		if ns, name, ok := strings.Cut(sa, ":"); !ok || ns == "" || name == "" {
			return fmt.Errorf("canI: serviceAccount must be \"namespace:name\", got %q", sa)
		}
	}
	return nil
}
