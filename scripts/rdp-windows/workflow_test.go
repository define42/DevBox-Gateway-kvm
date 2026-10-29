package rdpsmoke_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// These tests protect the distinction between compiling the harness and using
// Microsoft's client in a live session. actionlint validates workflow syntax.
type workflow struct {
	On   map[string]any         `yaml:"on"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

//nolint:tagliatelle // GitHub Actions uses kebab-case keys in its workflow schema.
type workflowJob struct {
	Uses            string            `yaml:"uses"`
	With            map[string]string `yaml:"with"`
	If              string            `yaml:"if"`
	ContinueOnError any               `yaml:"continue-on-error"`
	Secrets         any               `yaml:"secrets"`
	Steps           []workflowStep    `yaml:"steps"`
}

//nolint:tagliatelle // GitHub Actions uses kebab-case keys in its workflow schema.
type workflowStep struct {
	Uses            string `yaml:"uses"`
	Run             string `yaml:"run"`
	If              string `yaml:"if"`
	ContinueOnError any    `yaml:"continue-on-error"`
}

func readWorkflow(t *testing.T, name string) workflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var parsed workflow
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	return parsed
}

func TestCIRunsLiveMicrosoftRDP(t *testing.T) {
	t.Parallel()
	ci := readWorkflow(t, "ci.yml")
	var callers int
	for id, job := range ci.Jobs {
		if job.Uses != "./.github/workflows/rdp-windows.yml" {
			continue
		}
		callers++
		if job.With["mode"] != "both" || job.With["runner"] != "windows-2022" {
			t.Errorf("%s: CI must run both graphics modes on the Windows runner", id)
		}
		if job.If != "" || (job.ContinueOnError != nil && job.ContinueOnError != false) {
			t.Errorf("%s: live compatibility must not be optional or allowed to fail", id)
		}
		if job.Secrets != nil {
			t.Errorf("%s: loopback tests must not receive repository secrets", id)
		}
	}
	if callers != 1 {
		t.Errorf("want one same-commit live RDP workflow call, got %d", callers)
	}
}

func TestLiveMicrosoftRDPWorkflowRunsClientAndRetainsEvidence(t *testing.T) {
	t.Parallel()
	live := readWorkflow(t, "rdp-windows.yml")
	for _, trigger := range []string{"workflow_call", "workflow_dispatch"} {
		if _, ok := live.On[trigger]; !ok {
			t.Errorf("missing %s trigger", trigger)
		}
	}
	var liveSteps, evidenceSteps int
	for id, job := range live.Jobs {
		for _, step := range job.Steps {
			if strings.TrimSpace(step.Run) == "./scripts/rdp-windows/RunLocal.ps1" {
				liveSteps++
				if job.If != "" || (job.ContinueOnError != nil && job.ContinueOnError != false) ||
					step.If != "" || (step.ContinueOnError != nil && step.ContinueOnError != false) {
					t.Errorf("%s: the real Microsoft-client step must not be skipped or allowed to fail", id)
				}
			}
			if strings.HasPrefix(step.Uses, "actions/upload-artifact@") && step.If == "always()" {
				evidenceSteps++
			}
		}
	}
	if liveSteps != 1 || evidenceSteps != 1 {
		t.Errorf("want one live client step and one always-upload step, got %d and %d", liveSteps, evidenceSteps)
	}
}
