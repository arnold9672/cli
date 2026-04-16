// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package openclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// execRequest is the JSON payload sent to exec provider's stdin.
type execRequest struct {
	ProtocolVersion int      `json:"protocolVersion"`
	Provider        string   `json:"provider"`
	IDs             []string `json:"ids"`
}

// execResponse is the JSON payload expected from exec provider's stdout.
type execResponse struct {
	ProtocolVersion int                     `json:"protocolVersion"`
	Values          map[string]interface{}  `json:"values"`
	Errors          map[string]execRefError `json:"errors,omitempty"`
}

// execRefError is an optional per-id error in exec provider response.
type execRefError struct {
	Message string `json:"message"`
}

// resolveExecRef handles {source:"exec"} SecretRef resolution.
// Spawns the configured command with shell=false, sends a JSON request via stdin,
// reads JSON response from stdout, and extracts the secret value.
// Enforces timeout, maxOutputBytes, and security audit on command path.
func resolveExecRef(ref *SecretRef, pc *ProviderConfig, getenv func(string) string) (string, error) {
	if pc.Command == "" {
		return "", fmt.Errorf("exec provider command is empty")
	}

	// Security audit on command path
	securePath, err := AssertSecurePath(AuditParams{
		TargetPath:            pc.Command,
		Label:                 "exec provider command",
		TrustedDirs:           pc.TrustedDirs,
		AllowInsecurePath:     pc.AllowInsecurePath,
		AllowReadableByOthers: true, // exec: readable by others is OK (typical 755)
		AllowSymlinkPath:      pc.AllowSymlinkCommand,
	})
	if err != nil {
		return "", fmt.Errorf("exec provider security audit failed: %w", err)
	}

	// Build timeout
	timeoutMs := pc.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = DefaultExecTimeoutMs
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	maxOutput := pc.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = DefaultExecMaxOutputBytes
	}

	// Build child env (only passEnv + explicit env; NOT inheriting full parent env)
	childEnv := make([]string, 0, len(pc.PassEnv)+len(pc.Env))
	for _, key := range pc.PassEnv {
		if val := getenv(key); val != "" {
			childEnv = append(childEnv, key+"="+val)
		}
	}
	for key, val := range pc.Env {
		childEnv = append(childEnv, key+"="+val)
	}

	// Build request payload — use ref.Provider if set, else "default".
	// The full SecretsConfig is not passed to this function because
	// LookupProvider already resolved the provider config upstream.
	providerName := ref.Provider
	if providerName == "" {
		providerName = DefaultProviderAlias
	}
	reqPayload := execRequest{
		ProtocolVersion: 1,
		Provider:        providerName,
		IDs:             []string{ref.ID},
	}
	reqJSON, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("exec provider: failed to marshal request: %w", err)
	}

	// Spawn command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, securePath, pc.Args...)
	cmd.Dir = filepath.Dir(securePath)
	// ALWAYS set Env to restrict child to only passEnv + explicit env.
	// When Env is nil, Go inherits full parent env — that's a security gap.
	// OpenClaw resolve.ts always starts with an empty env object.
	cmd.Env = childEnv // may be empty slice — child gets minimal env
	cmd.Stdin = bytes.NewReader(reqJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("exec provider timed out after %dms", timeoutMs)
		}
		return "", fmt.Errorf("exec provider exited with error: %w", err)
	}

	// Check output size
	if stdout.Len() > maxOutput {
		return "", fmt.Errorf("exec provider output exceeded maxOutputBytes (%d)", maxOutput)
	}

	outBytes := stdout.Bytes()
	trimmed := bytes.TrimSpace(outBytes)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("exec provider returned empty stdout")
	}

	// Determine if jsonOnly mode
	jsonOnly := true
	if pc.JSONOnly != nil {
		jsonOnly = *pc.JSONOnly
	}

	// Try to parse as JSON response
	var resp execResponse
	if err := json.Unmarshal(trimmed, &resp); err != nil {
		// If jsonOnly=false and single ID, treat stdout as raw string value
		if !jsonOnly && len(reqPayload.IDs) == 1 {
			return string(trimmed), nil
		}
		return "", fmt.Errorf("exec provider returned invalid JSON: %w", err)
	}

	// Validate protocol version
	if resp.ProtocolVersion != 1 {
		return "", fmt.Errorf("exec provider protocolVersion must be 1, got %d", resp.ProtocolVersion)
	}

	// Check per-ref errors
	if resp.Errors != nil {
		if refErr, ok := resp.Errors[ref.ID]; ok {
			msg := refErr.Message
			if msg == "" {
				msg = "unknown error"
			}
			return "", fmt.Errorf("exec provider failed for id %q: %s", ref.ID, msg)
		}
	}

	// Extract value
	if resp.Values == nil {
		return "", fmt.Errorf("exec provider response missing 'values'")
	}

	value, ok := resp.Values[ref.ID]
	if !ok {
		return "", fmt.Errorf("exec provider response missing id %q", ref.ID)
	}

	strValue, ok := value.(string)
	if !ok {
		// jsonOnly=false + single ID: preserve JSON representation
		if !jsonOnly {
			data, err := json.Marshal(value)
			if err != nil {
				return "", fmt.Errorf("exec provider value for id %q is not JSON-serializable: %w", ref.ID, err)
			}
			return string(data), nil
		}
		return "", fmt.Errorf("exec provider value for id %q is not a string", ref.ID)
	}

	return strValue, nil
}
