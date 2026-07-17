package scaffold

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const serverKey = "agentorchestra"

// ClaudeDesktopConfigPath returns the platform's Claude Desktop config file
// location. AO_CLAUDE_DESKTOP_CONFIG overrides it (used by tests, and by
// anyone with a non-standard install).
func ClaudeDesktopConfigPath() (string, error) {
	if p := os.Getenv("AO_CLAUDE_DESKTOP_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
		}
		return filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json"), nil
	default:
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
	}
}

// SetupClaudeDesktop installs (or with remove=true, uninstalls) the
// agentorchestra MCP server entry in Claude Desktop's config.
//
// Safety rules (this file belongs to another app):
//   - the existing file is backed up to <path>.bak before every write
//   - only mcpServers.agentorchestra is touched; other servers and other
//     top-level keys are preserved as parsed
//   - if the existing file is not valid JSON, we refuse to write and tell
//     the caller to edit by hand — never overwrite something we can't parse
//
// It returns the config path and a human-readable action ("installed",
// "updated", "removed", "not installed").
func SetupClaudeDesktop(remove bool) (path string, action string, err error) {
	path, err = ClaudeDesktopConfigPath()
	if err != nil {
		return "", "", err
	}

	cfg := map[string]any{}
	existing, readErr := os.ReadFile(path)
	fileExists := readErr == nil
	if fileExists {
		if err := json.Unmarshal(existing, &cfg); err != nil {
			return path, "", fmt.Errorf("existing config at %s is not valid JSON (%v) — refusing to modify it; add the agentorchestra entry by hand or fix the file first", path, err)
		}
	} else if remove {
		return path, "not installed", nil
	}

	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	if remove {
		if _, ok := servers[serverKey]; !ok {
			return path, "not installed", nil
		}
		delete(servers, serverKey)
		action = "removed"
	} else {
		exe, err := os.Executable()
		if err != nil {
			return path, "", fmt.Errorf("locate ao binary: %w", err)
		}
		if _, ok := servers[serverKey]; ok {
			action = "updated"
		} else {
			action = "installed"
		}
		servers[serverKey] = map[string]any{
			"command": exe,
			"args":    []string{"mcp"},
		}
	}
	cfg["mcpServers"] = servers

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return path, "", err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return path, "", fmt.Errorf("create config dir: %w", err)
	}
	if fileExists {
		if err := os.WriteFile(path+".bak", existing, 0o644); err != nil {
			return path, "", fmt.Errorf("write backup %s.bak: %w — aborting without touching the config", path, err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return path, "", fmt.Errorf("write config: %w", err)
	}
	return path, action, nil
}
