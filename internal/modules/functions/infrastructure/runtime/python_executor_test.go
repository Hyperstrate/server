package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"hyperstrate/server/internal/modules/functions/application"
	"hyperstrate/server/internal/shared/dbtype"
)

func TestPythonExecutorExecutesEntrypointOutOfProcess(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte(`
def handle(payload):
    print("user log")
    return {"echo": payload["text"], "count": payload["count"] + 1}
`), 0o644); err != nil {
		t.Fatalf("write python module: %v", err)
	}

	executor := NewPythonExecutor(python)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := executor.Execute(ctx, application.ExecutionContract{
		Entrypoint:  "app.handle",
		Payload:     dbtype.JSONMap{"text": "hello", "count": float64(1)},
		TimeoutSecs: 5,
	}, ExecuteOptions{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.OK || result.Error != nil {
		t.Fatalf("unexpected execution result: %+v", result)
	}
	value, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatalf("result value = %#v", result.Value)
	}
	if value["echo"] != "hello" || value["count"] != float64(2) {
		t.Fatalf("unexpected result value: %+v", value)
	}
	if result.Stdout != "user log\n" {
		t.Fatalf("expected user stdout to be captured, got %q", result.Stdout)
	}
}

func TestPythonExecutorReturnsStructuredPythonError(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte(`
def handle(payload):
    raise ValueError("bad input")
`), 0o644); err != nil {
		t.Fatalf("write python module: %v", err)
	}

	executor := NewPythonExecutor(python)
	result, err := executor.Execute(context.Background(), application.ExecutionContract{
		Entrypoint:  "app.handle",
		Payload:     dbtype.JSONMap{},
		TimeoutSecs: 5,
	}, ExecuteOptions{WorkingDir: dir})
	if err != nil {
		t.Fatalf("Execute returned transport error: %v", err)
	}
	if result.OK || result.Error == nil {
		t.Fatalf("expected structured Python error, got %+v", result)
	}
	if result.Error.Type != "ValueError" || result.Error.Message != "bad input" {
		t.Fatalf("unexpected Python error: %+v", result.Error)
	}
}

func TestPythonExecutorRejectsInvalidEntrypoint(t *testing.T) {
	executor := NewPythonExecutor("python3")
	_, err := executor.Execute(context.Background(), application.ExecutionContract{
		Entrypoint: "missingdot",
	}, ExecuteOptions{})
	if !errors.Is(err, ErrInvalidEntrypoint) {
		t.Fatalf("expected ErrInvalidEntrypoint, got %v", err)
	}
}
