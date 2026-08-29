package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCorpusWorkflowPythonHeredocs(t *testing.T) {
	t.Parallel()

	workflow, err := os.ReadFile(".github/workflows/corpus.yml")
	if err != nil {
		t.Fatal(err)
	}

	for _, step := range []string{
		"Fetch previous generation",
		"Publish generation to the corpus branch",
	} {
		t.Run(step, func(t *testing.T) {
			script, err := workflowRunScript(string(workflow), step)
			if err != nil {
				t.Fatal(err)
			}

			command := exec.Command("bash", "-n")
			command.Stdin = strings.NewReader(script)
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Run(); err != nil {
				t.Fatalf("rendered run script does not parse: %v\n%s", err, stderr.String())
			}

			testCorpusWorkflowHeredoc(t, step, script)
		})
	}
}

func testCorpusWorkflowHeredoc(t *testing.T, step, script string) {
	t.Helper()

	dir := t.TempDir()
	var prefix string
	switch step {
	case "Fetch previous generation":
		root := filepath.Join(dir, "corpus-prev")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		writeWorkflowFixture(t, root, 6, []workflowPart{
			{Name: "corpus.jhtc.part-000", Offset: 0, Size: 3, Body: "abc"},
			{Name: "corpus.jhtc.part-001", Offset: 3, Size: 3, Body: "def"},
		})
	case "Publish generation to the corpus branch":
		root := filepath.Join(dir, "corpus-next")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		writeWorkflowFixture(t, root, 0, []workflowPart{
			{Name: "corpus.jhtc.part-000", Offset: 0, Size: 3, Body: "abc"},
			{Name: "corpus.jhtc.part-001", Offset: 3, Size: 2, Body: "de"},
		})
		prefix = "SIZE=5\n"
	default:
		t.Fatalf("no fixture for workflow step %q", step)
	}

	heredoc, err := pythonHeredoc(script)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash")
	command.Dir = dir
	command.Stdin = strings.NewReader(prefix + heredoc)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("execute rendered Python heredoc: %v\n%s", err, output)
	}

	if step == "Fetch previous generation" {
		got, err := os.ReadFile(filepath.Join(dir, "corpus-prev", "corpus.jhtc"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "abcdef" {
			t.Fatalf("reassembled corpus = %q, want abcdef", got)
		}
		return
	}

	raw, err := os.ReadFile(filepath.Join(dir, "corpus-next", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Transport map[string]struct {
			Size  int             `json:"size"`
			Parts []transportPart `json:"parts"`
		} `json:"transport"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	transport := manifest.Transport["corpus.jhtc"]
	want := []transportPart{
		{Name: "corpus.jhtc.part-000", Offset: 0, Size: 3},
		{Name: "corpus.jhtc.part-001", Offset: 3, Size: 2},
	}
	if transport.Size != 5 || !reflect.DeepEqual(transport.Parts, want) {
		t.Fatalf("transport = %+v, want size 5 and parts %+v", transport, want)
	}
}

type workflowPart struct {
	Name   string
	Offset int
	Size   int
	Body   string
}

type transportPart struct {
	Name   string `json:"name"`
	Offset int    `json:"offset"`
	Size   int    `json:"size"`
}

func writeWorkflowFixture(t *testing.T, root string, size int, parts []workflowPart) {
	t.Helper()

	manifest := map[string]any{"format_version": 1}
	if size > 0 {
		transport := make([]transportPart, 0, len(parts))
		for _, part := range parts {
			transport = append(transport, transportPart{Name: part.Name, Offset: part.Offset, Size: part.Size})
		}
		manifest["transport"] = map[string]any{
			"corpus.jhtc": map[string]any{"size": size, "parts": transport},
		}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if err := os.WriteFile(filepath.Join(root, part.Name), []byte(part.Body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func pythonHeredoc(script string) (string, error) {
	lines := strings.Split(script, "\n")
	for start, line := range lines {
		if !strings.Contains(line, "python3 - <<'PY'") {
			continue
		}
		for end := start + 1; end < len(lines); end++ {
			if lines[end] == "PY" {
				return strings.Join(lines[start:end+1], "\n") + "\n", nil
			}
		}
	}
	return "", fmt.Errorf("rendered script has no complete Python heredoc")
}

func workflowRunScript(workflow, stepName string) (string, error) {
	lines := strings.Split(workflow, "\n")
	name := "- name: " + stepName
	for i, line := range lines {
		if strings.TrimSpace(line) != name {
			continue
		}

		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) != "run: |" {
				continue
			}

			indent := 0
			for _, bodyLine := range lines[j+1:] {
				if strings.TrimSpace(bodyLine) != "" {
					indent = len(bodyLine) - len(strings.TrimLeft(bodyLine, " "))
					break
				}
			}
			if indent == 0 {
				return "", fmt.Errorf("workflow step %q has an empty run script", stepName)
			}
			var script strings.Builder
			for _, bodyLine := range lines[j+1:] {
				if bodyLine != "" && len(bodyLine)-len(strings.TrimLeft(bodyLine, " ")) < indent {
					break
				}
				if len(bodyLine) >= indent {
					bodyLine = bodyLine[indent:]
				}
				script.WriteString(bodyLine)
				script.WriteByte('\n')
			}
			return script.String(), nil
		}
	}

	return "", fmt.Errorf("workflow step %q run script not found", stepName)
}
