// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/keychain"
	"github.com/larksuite/cli/internal/openclaw"
	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/validate"
	"github.com/larksuite/cli/internal/vfs"
)

// BindOptions holds all inputs for config bind.
type BindOptions struct {
	Factory      *cmdutil.Factory
	Source       string
	AppID        string
	StrictMode   string
	DefaultAs    string
	Lang         string
	langExplicit bool // true when --lang was explicitly passed
	Force        bool
}

// NewCmdConfigBind creates the config bind subcommand.
func NewCmdConfigBind(f *cmdutil.Factory, runF func(*BindOptions) error) *cobra.Command {
	opts := &BindOptions{Factory: f}

	cmd := &cobra.Command{
		Use:   "bind",
		Short: "Bind Agent config to a workspace (source / app-id / force)",
		Long: `Bind an AI Agent's (OpenClaw / Hermes) Feishu credentials to a lark-cli workspace.

For AI agents: pass --source and --app-id to bind non-interactively.
Credentials are synced once; subsequent calls in the Agent's process
context automatically use the bound workspace.`,
		Example: `  lark-cli config bind --source openclaw --app-id <id>
  lark-cli config bind --source hermes
  lark-cli config bind --source openclaw --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.langExplicit = cmd.Flags().Changed("lang")
			if runF != nil {
				return runF(opts)
			}
			return configBindRun(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Source, "source", "", "Agent source to bind from (openclaw|hermes)")
	cmd.Flags().StringVar(&opts.AppID, "app-id", "", "App ID to bind (required for OpenClaw multi-account)")
	cmd.Flags().StringVar(&opts.StrictMode, "strict-mode", "", "strict mode policy (bot|user|off)")
	cmd.Flags().StringVar(&opts.DefaultAs, "default-as", "", "default identity (user|bot|auto)")
	cmd.Flags().StringVar(&opts.Lang, "lang", "zh", "language for interactive prompts (zh|en)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "force bind even if workspace already exists")

	return cmd
}

// configBindRun is the main bind execution flow.
func configBindRun(opts *BindOptions) error {
	// Validate enum flags early — before any side effects (keychain writes, etc.)
	if err := validateBindFlags(opts); err != nil {
		return err
	}

	source := strings.TrimSpace(strings.ToLower(opts.Source))

	// Determine mode: TUI (interactive) vs Flag (non-interactive)
	isTUI := source == "" && opts.Factory.IOStreams.IsTerminal

	if isTUI {
		// ── TUI Mode ──
		// Step 0: Language selection (same as config init — prompt if --lang not explicit)
		if !opts.langExplicit {
			lang, langErr := promptLangSelection("")
			if langErr != nil {
				if langErr == huh.ErrUserAborted {
					return output.ErrBare(1)
				}
				return langErr
			}
			opts.Lang = lang
		}

		// Step 1: Source selection
		var err error
		source, err = tuiSelectSource(opts)
		if err != nil {
			return err
		}
	} else if source == "" {
		// Non-TTY and no --source → error
		return output.ErrWithHint(output.ExitValidation, "bind",
			"--source is required (openclaw or hermes)",
			"lark-cli config bind --source openclaw")
	}

	if source != "openclaw" && source != "hermes" {
		return output.ErrValidation("invalid --source %q; valid values: openclaw, hermes", source)
	}

	// Set workspace to target source (FR-038: bind's own artifacts follow target workspace)
	ws := core.Workspace(source)
	core.SetCurrentWorkspace(ws)

	// Check for existing binding
	targetDir := core.GetConfigDir()
	targetConfigPath := core.GetConfigPath()
	// Read old config data before overwriting (for deferred keychain cleanup).
	oldConfigData, _ := vfs.ReadFile(targetConfigPath)
	if oldConfigData != nil {
		// Config already exists — handle conflict
		if opts.Force {
			// proceed (old keychain cleaned up after successful write)
		} else if isTUI {
			// TUI: prompt user to choose
			action, tuiErr := tuiConflictPrompt(opts, source, targetConfigPath)
			if tuiErr != nil {
				return tuiErr
			}
			if action == "cancel" {
				msg := getBindMsg(opts.Lang)
				fmt.Fprintln(opts.Factory.IOStreams.ErrOut, msg.ConflictCancelled)
				return nil
			}
			// action == "force" → proceed (old keychain cleaned up after successful write)
		} else {
			return output.ErrWithHint(output.ExitValidation, "bind",
				fmt.Sprintf("workspace %q already bound at %s", source, targetConfigPath),
				"pass --force to replace, or run 'lark-cli config bind' (no flags) for interactive mode")
		}
	}

	// TUI: show security disclaimer before executing bind
	if isTUI {
		if disclaimerErr := tuiSecurityDisclaimer(opts, source); disclaimerErr != nil {
			return disclaimerErr
		}
	}

	// Dispatch to source-specific bind flow
	var appConfig *core.AppConfig
	var err error

	switch source {
	case "openclaw":
		appConfig, err = bindOpenClaw(opts)
	case "hermes":
		appConfig, err = bindHermes(opts)
	default:
		return output.ErrValidation("unsupported source: %s", source)
	}
	if err != nil {
		return err
	}

	// TUI: prompt for default-as and strict-mode if not passed via flags
	if isTUI {
		if opts.DefaultAs == "" {
			da, tuiErr := tuiSelectDefaultAs(opts)
			if tuiErr != nil {
				return tuiErr
			}
			opts.DefaultAs = da
		}
		if opts.StrictMode == "" {
			sm, tuiErr := tuiSelectStrictMode(opts)
			if tuiErr != nil {
				return tuiErr
			}
			opts.StrictMode = sm
		}
	}

	// Apply overrides (already validated by validateBindFlags)
	if opts.StrictMode != "" {
		sm := core.StrictMode(opts.StrictMode)
		appConfig.StrictMode = &sm
	}
	if opts.DefaultAs != "" {
		appConfig.DefaultAs = core.Identity(opts.DefaultAs)
	}
	if opts.Lang != "" {
		appConfig.Lang = opts.Lang
	}

	// Build MultiAppConfig for workspace
	// Note: secret is already stored in keychain by bindOpenClaw/bindHermes.
	// Don't set CurrentApp — single-app workspace falls back to Apps[0]
	// (aligned with config init behavior, Principle III).
	multi := &core.MultiAppConfig{
		Apps: []core.AppConfig{*appConfig},
	}

	// Ensure target workspace directory exists
	if err := vfs.MkdirAll(targetDir, 0700); err != nil {
		return output.Errorf(output.ExitInternal, "bind",
			"failed to create workspace directory %s: %v", targetDir, err)
	}

	// Atomic write config.json
	data, err := json.MarshalIndent(multi, "", "  ")
	if err != nil {
		return output.Errorf(output.ExitInternal, "bind",
			"failed to marshal config: %v", err)
	}
	if err := validate.AtomicWrite(targetConfigPath, append(data, '\n'), 0600); err != nil {
		return output.Errorf(output.ExitInternal, "bind",
			"failed to write config %s: %v", targetConfigPath, err)
	}

	// Cleanup old keychain entries only after new config is persisted (FR-036).
	// This ensures the old workspace remains usable if bind fails mid-way.
	// Uses saved oldConfigData (read before overwrite) to identify stale entries.
	if oldConfigData != nil {
		cleanupKeychainFromData(opts.Factory.Keychain, oldConfigData)
	}

	// Success output (JSON envelope for AI consumers)
	result := map[string]interface{}{
		"ok":          true,
		"workspace":   source,
		"app_id":      appConfig.AppId,
		"config_path": targetConfigPath,
	}
	resultJSON, _ := json.Marshal(result)
	fmt.Fprintln(opts.Factory.IOStreams.Out, string(resultJSON))

	return nil
}

// bindOpenClaw reads OpenClaw config and resolves credentials.
func bindOpenClaw(opts *BindOptions) (*core.AppConfig, error) {
	// Resolve openclaw.json path — align with OpenClaw src/config/paths.ts:
	// Priority: OPENCLAW_CONFIG_PATH → OPENCLAW_STATE_DIR/openclaw.json
	//         → OPENCLAW_HOME/.openclaw/openclaw.json → ~/.openclaw/openclaw.json
	//         → legacy ~/.clawdbot/clawdbot.json
	openclawPath := resolveOpenClawConfigPath()
	cfg, err := openclaw.ReadOpenClawConfig(openclawPath)
	if err != nil {
		return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
			fmt.Sprintf("cannot read %s: %v", openclawPath, err),
			"verify OpenClaw is installed and configured")
	}

	if cfg.Channels.Feishu == nil {
		return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
			"openclaw.json missing channels.feishu section",
			"configure Feishu in OpenClaw first")
	}

	// List candidate apps
	candidates := openclaw.ListCandidateApps(cfg.Channels.Feishu)
	if len(candidates) == 0 {
		return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
			"no Feishu app configured in openclaw.json",
			"configure channels.feishu.appId in openclaw.json")
	}

	// Select app
	var selected *openclaw.CandidateApp
	if opts.AppID != "" {
		// Flag mode: strict match (FR-021)
		for i := range candidates {
			if candidates[i].AppID == opts.AppID {
				selected = &candidates[i]
				break
			}
		}
		if selected == nil {
			ids := make([]string, 0, len(candidates))
			for _, c := range candidates {
				label := c.AppID
				if c.Label != "" {
					label = fmt.Sprintf("%s (%s)", c.AppID, c.Label)
				}
				ids = append(ids, label)
			}
			return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
				fmt.Sprintf("--app-id %q not found in openclaw.json", opts.AppID),
				fmt.Sprintf("available app IDs:\n  %s", strings.Join(ids, "\n  ")))
		}
	} else if len(candidates) == 1 {
		selected = &candidates[0]
	} else if opts.Factory.IOStreams.IsTerminal {
		// TUI mode: let user choose from candidates
		var tuiErr error
		selected, tuiErr = tuiSelectApp(opts, candidates)
		if tuiErr != nil {
			return nil, tuiErr
		}
	} else {
		// Flag mode: multi-account without --app-id → error with candidate list
		ids := make([]string, 0, len(candidates))
		for _, c := range candidates {
			label := c.AppID
			if c.Label != "" {
				label = fmt.Sprintf("%s (%s)", c.AppID, c.Label)
			}
			ids = append(ids, label)
		}
		return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
			"multiple accounts in openclaw.json; pass --app-id <id>",
			fmt.Sprintf("available app IDs:\n  %s", strings.Join(ids, "\n  ")))
	}

	// Resolve secret
	if selected.AppSecret.IsZero() {
		return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
			fmt.Sprintf("appSecret is empty for app %s in %s", selected.AppID, openclawPath),
			"configure channels.feishu.appSecret in openclaw.json")
	}
	secret, err := openclaw.ResolveSecretInput(selected.AppSecret, cfg.Secrets, os.Getenv)
	if err != nil {
		return nil, output.ErrWithHint(output.ExitValidation, "openclaw",
			fmt.Sprintf("failed to resolve appSecret for %s: %v", selected.AppID, err),
			fmt.Sprintf("check appSecret configuration in %s", openclawPath))
	}

	// Normalize brand
	brand := normalizeBrand(selected.Brand)

	// Store in keychain
	secretInput := core.PlainSecret(secret)
	stored, err := core.ForStorage(selected.AppID, secretInput, opts.Factory.Keychain)
	if err != nil {
		return nil, output.Errorf(output.ExitInternal, "openclaw",
			"keychain unavailable: %v\nhint: use file: reference in config to bypass keychain", err)
	}

	return &core.AppConfig{
		AppId:     selected.AppID,
		AppSecret: stored,
		Brand:     core.LarkBrand(brand),
	}, nil
}

// bindHermes reads Feishu credentials from Hermes's local dotenv file (~/.hermes/.env).
func bindHermes(opts *BindOptions) (*core.AppConfig, error) {
	// Resolve Hermes .env path (respect HERMES_HOME override)
	envPath := resolveHermesEnvPath()

	// Read and parse dotenv file
	envMap, err := readDotenv(envPath)
	if err != nil {
		return nil, output.ErrWithHint(output.ExitValidation, "hermes",
			fmt.Sprintf("failed to read Hermes config: %v", err),
			fmt.Sprintf("verify Hermes is installed and configured at %s", envPath))
	}

	appID := envMap["FEISHU_APP_ID"]
	appSecret := envMap["FEISHU_APP_SECRET"]

	if appID == "" {
		return nil, output.ErrWithHint(output.ExitValidation, "hermes",
			fmt.Sprintf("FEISHU_APP_ID not found in %s", envPath),
			"run 'hermes setup' to configure Feishu credentials")
	}
	if appSecret == "" {
		return nil, output.ErrWithHint(output.ExitValidation, "hermes",
			fmt.Sprintf("FEISHU_APP_SECRET not found in %s", envPath),
			"run 'hermes setup' to configure Feishu credentials")
	}

	// Normalize brand (FR-022: .strip().lower(), default "feishu")
	brand := normalizeBrand(envMap["FEISHU_DOMAIN"])

	// Store in keychain
	secretInput := core.PlainSecret(appSecret)
	stored, err := core.ForStorage(appID, secretInput, opts.Factory.Keychain)
	if err != nil {
		return nil, output.Errorf(output.ExitInternal, "hermes",
			"keychain unavailable: %v\nhint: use file: reference in config to bypass keychain", err)
	}

	return &core.AppConfig{
		AppId:     appID,
		AppSecret: stored,
		Brand:     core.LarkBrand(brand),
	}, nil
}

// normalizeBrand applies .strip().lower() and defaults to "feishu".
// Aligns with Hermes gateway/platforms/feishu.py:1119 behavior.
func normalizeBrand(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return "feishu"
	}
	return s
}

// cleanupKeychainFromData removes keychain entries based on previously-read config data.
// Called after new config is persisted to prevent stale entries (FR-036).
// Best-effort: errors are silently ignored (aligned with config init's cleanupOldConfig).
func cleanupKeychainFromData(kc keychain.KeychainAccess, data []byte) {
	var multi core.MultiAppConfig
	if err := json.Unmarshal(data, &multi); err != nil {
		return
	}
	for _, app := range multi.Apps {
		core.RemoveSecretStore(app.AppSecret, kc)
	}
}

// ──────────────────────────────────────────────────────────────
// TUI helpers (huh forms, matching config init interactive style)
// ──────────────────────────────────────────────────────────────

// tuiSelectSource prompts user to choose bind source.
func tuiSelectSource(opts *BindOptions) (string, error) {
	msg := getBindMsg(opts.Lang)
	var source string

	// Pre-select based on detected env signals
	detected := core.DetectWorkspaceFromEnv(os.Getenv)
	switch detected {
	case core.WorkspaceOpenClaw:
		source = "openclaw"
	case core.WorkspaceHermes:
		source = "hermes"
	default:
		source = "openclaw" // default first option
	}

	// Resolve actual paths for display
	openclawPath := resolveOpenClawConfigPath()
	hermesEnvPath := resolveHermesEnvPath()

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(msg.SelectSource).
				Description(msg.SelectSourceDesc).
				Options(
					huh.NewOption(fmt.Sprintf(msg.SourceOpenClaw, openclawPath), "openclaw"),
					huh.NewOption(fmt.Sprintf(msg.SourceHermes, hermesEnvPath), "hermes"),
				).
				Value(&source),
		),
	).WithTheme(cmdutil.ThemeFeishu())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "", output.ErrBare(1)
		}
		return "", err
	}
	return source, nil
}

// tuiSelectApp prompts user to choose from multiple OpenClaw accounts.
func tuiSelectApp(opts *BindOptions, candidates []openclaw.CandidateApp) (*openclaw.CandidateApp, error) {
	msg := getBindMsg(opts.Lang)
	options := make([]huh.Option[int], 0, len(candidates))
	for i, c := range candidates {
		label := c.AppID
		if c.Label != "" {
			label = fmt.Sprintf("%s (%s)", c.Label, c.AppID)
		}
		options = append(options, huh.NewOption(label, i))
	}

	var selected int
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(msg.SelectAccount).
				Options(options...).
				Value(&selected),
		),
	).WithTheme(cmdutil.ThemeFeishu())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return nil, output.ErrBare(1)
		}
		return nil, err
	}
	return &candidates[selected], nil
}

// tuiConflictPrompt shows existing binding and asks user to Force or Cancel.
func tuiConflictPrompt(opts *BindOptions, source, configPath string) (string, error) {
	msg := getBindMsg(opts.Lang)

	// Build existing binding summary
	existingSummary := fmt.Sprintf(msg.ConflictDesc, source, "?", "?", configPath)
	if data, err := vfs.ReadFile(configPath); err == nil {
		var multi core.MultiAppConfig
		if json.Unmarshal(data, &multi) == nil && len(multi.Apps) > 0 {
			app := multi.Apps[0]
			existingSummary = fmt.Sprintf(msg.ConflictDesc,
				source, app.AppId, app.Brand, configPath)
		}
	}

	var action string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(msg.ConflictTitle).
				Description(existingSummary),
			huh.NewSelect[string]().
				Options(
					huh.NewOption(msg.ConflictForce, "force"),
					huh.NewOption(msg.ConflictCancel, "cancel"),
				).
				Value(&action),
		),
	).WithTheme(cmdutil.ThemeFeishu())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "cancel", nil
		}
		return "", err
	}
	return action, nil
}

// tuiSecurityDisclaimer shows a note about what bind does (security awareness).
func tuiSecurityDisclaimer(opts *BindOptions, source string) error {
	msg := getBindMsg(opts.Lang)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title(msg.SecurityTitle).
				Description(fmt.Sprintf(msg.SecurityDesc,
					source, core.GetConfigPath(), source)),
		),
	).WithTheme(cmdutil.ThemeFeishu())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return output.ErrBare(1)
		}
		return err
	}
	return nil
}

// validateBindFlags validates enum flags early, before any side effects.
func validateBindFlags(opts *BindOptions) error {
	if opts.StrictMode != "" {
		switch opts.StrictMode {
		case "bot", "user", "off":
		default:
			return output.ErrValidation("invalid --strict-mode %q; valid values: bot, user, off", opts.StrictMode)
		}
	}
	if opts.DefaultAs != "" {
		switch opts.DefaultAs {
		case "user", "bot", "auto":
		default:
			return output.ErrValidation("invalid --default-as %q; valid values: user, bot, auto", opts.DefaultAs)
		}
	}
	return nil
}

// tuiSelectStrictMode prompts user to choose strict mode policy.
func tuiSelectStrictMode(opts *BindOptions) (string, error) {
	msg := getBindMsg(opts.Lang)
	var value string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(msg.SelectStrictMode).
				Description(msg.SelectStrictModeDesc).
				Options(
					huh.NewOption(msg.StrictModeOff, "off"),
					huh.NewOption(msg.StrictModeBot, "bot"),
					huh.NewOption(msg.StrictModeUser, "user"),
				).
				Value(&value),
		),
	).WithTheme(cmdutil.ThemeFeishu())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "", output.ErrBare(1)
		}
		return "", err
	}
	return value, nil
}

// tuiSelectDefaultAs prompts user to choose default identity.
func tuiSelectDefaultAs(opts *BindOptions) (string, error) {
	msg := getBindMsg(opts.Lang)
	var value string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(msg.SelectDefaultAs).
				Description(msg.SelectDefaultAsDesc).
				Options(
					huh.NewOption(msg.DefaultAsAuto, "auto"),
					huh.NewOption(msg.DefaultAsUser, "user"),
					huh.NewOption(msg.DefaultAsBot, "bot"),
				).
				Value(&value),
		),
	).WithTheme(cmdutil.ThemeFeishu())

	if err := form.Run(); err != nil {
		if err == huh.ErrUserAborted {
			return "", output.ErrBare(1)
		}
		return "", err
	}
	return value, nil
}

// ──────────────────────────────────────────────────────────────
// Hermes config path resolution
// ──────────────────────────────────────────────────────────────

// resolveHermesEnvPath returns the path to Hermes's .env file.
// Respects HERMES_HOME override; defaults to ~/.hermes/.env.
//
// Note: HERMES_HOME is typically unset when users run bind from a regular terminal
// (most users use the default ~/.hermes path). However, when AI agents execute bind
// within a Hermes subprocess (e.g., from a chat dialog), HERMES_HOME may be set and
// should be respected to locate the correct config.
func resolveHermesEnvPath() string {
	hermesHome := os.Getenv("HERMES_HOME")
	if hermesHome == "" {
		home, err := vfs.UserHomeDir()
		if err != nil || home == "" {
			fmt.Fprintf(os.Stderr, "warning: unable to determine home directory: %v\n", err)
		}
		hermesHome = filepath.Join(home, ".hermes")
	}
	return filepath.Join(hermesHome, ".env")
}

// ──────────────────────────────────────────────────────────────
// OpenClaw config path resolution (aligned with src/config/paths.ts)
// ──────────────────────────────────────────────────────────────

// resolveOpenClawConfigPath resolves the openclaw.json path using the same
// priority chain as OpenClaw's src/config/paths.ts:
//  1. OPENCLAW_CONFIG_PATH env → exact file path
//  2. OPENCLAW_STATE_DIR env → <dir>/openclaw.json
//  3. OPENCLAW_HOME env → <home>/.openclaw/openclaw.json
//  4. ~/.openclaw/openclaw.json (default)
//  5. Legacy fallbacks: ~/.clawdbot/clawdbot.json, ~/.openclaw/clawdbot.json
//
// Note: OPENCLAW_CONFIG_PATH / OPENCLAW_STATE_DIR / OPENCLAW_HOME are typically
// unset when users run bind from a regular terminal (most users use the default
// ~/.openclaw path). However, when AI agents execute bind within an OpenClaw
// subprocess (e.g., from a chat dialog), these env vars may be set by OpenClaw
// and should be respected to locate the correct config for that workspace.
func resolveOpenClawConfigPath() string {
	// 1. Explicit config path override
	if p := os.Getenv("OPENCLAW_CONFIG_PATH"); p != "" {
		return expandHome(p)
	}

	// 2. State dir override
	if stateDir := os.Getenv("OPENCLAW_STATE_DIR"); stateDir != "" {
		dir := expandHome(stateDir)
		return findConfigInDir(dir)
	}

	// 3. Home override
	home := os.Getenv("OPENCLAW_HOME")
	if home == "" {
		h, err := vfs.UserHomeDir()
		if err != nil || h == "" {
			fmt.Fprintf(os.Stderr, "warning: unable to determine home directory: %v\n", err)
		}
		home = h
	} else {
		home = expandHome(home)
	}

	// 4. Default: ~/.openclaw/openclaw.json
	newDir := filepath.Join(home, ".openclaw")
	if configFile := findConfigInDir(newDir); fileExists(configFile) {
		return configFile
	}

	// 5. Legacy: ~/.clawdbot/
	legacyDir := filepath.Join(home, ".clawdbot")
	if configFile := findConfigInDir(legacyDir); fileExists(configFile) {
		return configFile
	}

	// Default: ~/.openclaw/openclaw.json (even if doesn't exist yet — will error in reader)
	return filepath.Join(newDir, "openclaw.json")
}

// findConfigInDir checks for openclaw.json then legacy clawdbot.json in a directory.
func findConfigInDir(dir string) string {
	primary := filepath.Join(dir, "openclaw.json")
	if fileExists(primary) {
		return primary
	}
	legacy := filepath.Join(dir, "clawdbot.json")
	if fileExists(legacy) {
		return legacy
	}
	return primary // default to primary even if missing
}

func fileExists(path string) bool {
	_, err := vfs.Stat(path)
	return err == nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := vfs.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// ──────────────────────────────────────────────────────────────
// Dotenv parser (for reading Hermes ~/.hermes/.env)
// ──────────────────────────────────────────────────────────────

// readDotenv reads a dotenv file and returns key-value pairs.
// Format: KEY=VALUE per line; # comments; empty lines ignored.
// Matches Hermes's load_env() in hermes_cli/config.py.
func readDotenv(path string) (map[string]string, error) {
	data, err := vfs.ReadFile(path)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key != "" {
			result[key] = value
		}
	}
	return result, nil
}
