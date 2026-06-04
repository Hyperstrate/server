package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hyperstrate/server/internal/modules/functions/application"
)

var ErrInvalidEntrypoint = errors.New("invalid Python entrypoint")

type PythonExecutor struct {
	binary string
}

type ExecuteOptions struct {
	WorkingDir string
}

type ExecutionResult struct {
	OK     bool         `json:"ok"`
	Value  any          `json:"value,omitempty"`
	Error  *PythonError `json:"error,omitempty"`
	Stdout string       `json:"stdout,omitempty"`
	Stderr string       `json:"stderr,omitempty"`
}

type PythonError struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	Traceback string `json:"traceback,omitempty"`
}

func NewPythonExecutor(binary string) *PythonExecutor {
	if strings.TrimSpace(binary) == "" {
		binary = "python3"
	}
	return &PythonExecutor{binary: binary}
}

func (e *PythonExecutor) Execute(ctx context.Context, contract application.ExecutionContract, opts ExecuteOptions) (*ExecutionResult, error) {
	if !validEntrypoint(contract.Entrypoint) {
		return nil, ErrInvalidEntrypoint
	}
	if contract.TimeoutSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(contract.TimeoutSecs)*time.Second)
		defer cancel()
	}
	payload, err := json.Marshal(contract.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal execution payload: %w", err)
	}
	resultFile, cleanup, err := tempResultFile(opts.WorkingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, e.binary, "-c", pythonWrapper)
	cmd.Dir = opts.WorkingDir
	cmd.Env = pythonEnv(os.Environ(), opts.WorkingDir, contract.Entrypoint, string(payload), resultFile)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result, readErr := readExecutionResult(resultFile)
	if readErr != nil {
		if runErr != nil {
			return nil, fmt.Errorf("run python: %w; read result: %v", runErr, readErr)
		}
		return nil, readErr
	}
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	return result, nil
}

func validEntrypoint(entrypoint string) bool {
	parts := strings.Split(entrypoint, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func tempResultFile(workingDir string) (string, func(), error) {
	dir := workingDir
	if dir == "" {
		dir = os.TempDir()
	}
	file, err := os.CreateTemp(dir, "hyperstrate-result-*.json")
	if err != nil {
		return "", func() {}, fmt.Errorf("create result file: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", func() {}, fmt.Errorf("close result file: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func readExecutionResult(path string) (*ExecutionResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read execution result: %w", err)
	}
	var result ExecutionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode execution result: %w", err)
	}
	return &result, nil
}

func pythonEnv(base []string, workingDir, entrypoint, payload, resultFile string) []string {
	env := append([]string{}, base...)
	env = append(env,
		"HYPERSTRATE_ENTRYPOINT="+entrypoint,
		"HYPERSTRATE_PAYLOAD="+payload,
		"HYPERSTRATE_RESULT_FILE="+resultFile,
	)
	if workingDir != "" {
		sep := ":"
		if runtime.GOOS == "windows" {
			sep = ";"
		}
		pythonPath := workingDir
		for _, item := range base {
			if strings.HasPrefix(item, "PYTHONPATH=") {
				pythonPath = pythonPath + sep + strings.TrimPrefix(item, "PYTHONPATH=")
				break
			}
		}
		env = append(env, "PYTHONPATH="+filepath.Clean(pythonPath))
	}
	return env
}

const pythonWrapper = `
import importlib
import json
import os
import traceback

entrypoint = os.environ["HYPERSTRATE_ENTRYPOINT"]
payload = json.loads(os.environ.get("HYPERSTRATE_PAYLOAD", "{}"))
result_file = os.environ["HYPERSTRATE_RESULT_FILE"]
module_name, callable_name = entrypoint.rsplit(".", 1)

try:
    module = importlib.import_module(module_name)
    fn = getattr(module, callable_name)
    value = fn(payload)
    envelope = {"ok": True, "value": value}
except BaseException as exc:
    envelope = {
        "ok": False,
        "error": {
            "type": exc.__class__.__name__,
            "message": str(exc),
            "traceback": traceback.format_exc(),
        },
    }

with open(result_file, "w", encoding="utf-8") as fh:
    json.dump(envelope, fh)
`
