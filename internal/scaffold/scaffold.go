// Package scaffold generates the per-project glue files that make
// AgentOrchestra a one-command setup: .mcp.json (Claude Code / OpenCode),
// an AgentOrchestra section in CLAUDE.md, an AGENTS.md for other agents,
// and .gitignore entries for ephemeral .ao/ subdirectories.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

const (
	markerBegin = "<!-- agentorchestra:begin -->"
	markerEnd   = "<!-- agentorchestra:end -->"
)

// TeamPreset is the ready-made roster for the user's actual team — pass to
// Init so no YAML has to be written by hand. Edit .agentconfig afterwards
// to taste.
func TeamPreset() []workspace.Agent {
	return []workspace.Agent{
		{
			Name:         "claude-code",
			Description:  "วางแผนสถาปัตยกรรมและเขียนโค้ด (Go/Python/อื่นๆ)",
			Capabilities: []string{"planning", "coding", "refactoring", "review"},
		},
		{
			Name:         "codex",
			Description:  "วางแผนและเขียนโค้ด ใช้สลับ/เสริมกับ claude-code",
			Capabilities: []string{"planning", "coding"},
		},
		{
			Name:         "antigravity-ide",
			Description:  "สร้าง infographic / diagram / เอกสารภาพจากสเปคและโค้ด",
			Capabilities: []string{"infographic", "diagram", "documentation"},
		},
		{
			Name:         "ollama-worker",
			Description:  "โมเดลเล็ก (local LLM) ทำงานย่อยตามสั่ง เช่น สรุปความ แปลง format เขียน docstring — รับงานผ่าน `ao worker`",
			Capabilities: []string{"summarize", "small-tasks", "translation"},
		},
	}
}

// Apply generates all glue files in projectDir. Idempotent: existing
// .mcp.json / AGENTS.md are left alone, and the CLAUDE.md section is
// replaced in place (marker-delimited) rather than appended twice.
func Apply(projectDir string, agents []workspace.Agent) error {
	if err := writeMCPJSON(projectDir); err != nil {
		return err
	}
	if err := upsertClaudeMD(projectDir, agents); err != nil {
		return err
	}
	if err := writeAgentsMD(projectDir, agents); err != nil {
		return err
	}
	return ensureGitignore(projectDir)
}

func writeMCPJSON(projectDir string) error {
	path := filepath.Join(projectDir, ".mcp.json")
	if _, err := os.Stat(path); err == nil {
		return nil // never clobber an existing MCP config
	}
	content := `{
  "mcpServers": {
    "agentorchestra": {
      "command": "ao",
      "args": ["mcp"]
    }
  }
}
`
	return os.WriteFile(path, []byte(content), 0o644)
}

func instructionBody(agents []workspace.Agent) string {
	var b strings.Builder
	b.WriteString("## AgentOrchestra — กติกาการทำงานร่วมกับ agent อื่น\n\n")
	b.WriteString("โปรเจกต์นี้ใช้ AgentOrchestra (MCP tools `ao_*`) ประสานงานหลาย agent:\n\n")
	b.WriteString("1. **เริ่ม session:** เรียก `ao_agents` เพื่อดูว่าทีมมีใครบ้าง ใครถนัดอะไร ใคร online แล้วเรียก `ao_status` ดูว่างานถึงตาใคร\n")
	b.WriteString("2. **ก่อนเริ่มงานชิ้นใหม่:** ถ้า `ao_status` บอกว่าไม้อยู่ที่คุณ ให้ทำ task ที่ระบุไว้ พร้อมอ่านไฟล์ใน artifacts และ notes\n")
	b.WriteString("3. **จบงานทุกครั้ง:** เรียก `ao_handoff` ระบุ target_agent (เลือกจาก capabilities ของทีม), task ที่ให้ทำต่อ, notes อธิบายบริบท, และ artifacts\n")
	b.WriteString("4. ระบุตัวเองด้วย arg `agent` ตอนเรียก `ao_status`/`ao_agents` เพื่อให้เพื่อนร่วมทีมเห็นว่าคุณ online\n")
	if len(agents) > 0 {
		b.WriteString("\nทีมของโปรเจกต์นี้:\n")
		for _, a := range agents {
			line := "- `" + a.Name + "`"
			if len(a.Capabilities) > 0 {
				line += " (" + strings.Join(a.Capabilities, ", ") + ")"
			}
			if a.Description != "" {
				line += " — " + a.Description
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// upsertClaudeMD adds (or replaces) the marker-delimited AgentOrchestra
// section in CLAUDE.md, creating the file if needed.
func upsertClaudeMD(projectDir string, agents []workspace.Agent) error {
	path := filepath.Join(projectDir, "CLAUDE.md")
	section := markerBegin + "\n" + instructionBody(agents) + markerEnd + "\n"

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return os.WriteFile(path, []byte(section), 0o644)
	}
	if err != nil {
		return fmt.Errorf("read CLAUDE.md: %w", err)
	}

	content := string(data)
	begin := strings.Index(content, markerBegin)
	end := strings.Index(content, markerEnd)
	if begin >= 0 && end > begin {
		content = content[:begin] + section + content[end+len(markerEnd)+1:]
	} else {
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + section
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// writeAgentsMD creates AGENTS.md (read by Codex, OpenCode, Antigravity and
// others) with the same instructions. Existing files are left untouched.
func writeAgentsMD(projectDir string, agents []workspace.Agent) error {
	path := filepath.Join(projectDir, "AGENTS.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(instructionBody(agents)), 0o644)
}

// ensureGitignore appends the ephemeral .ao/ subpaths to the project's
// .gitignore (creating it if needed), once.
func ensureGitignore(projectDir string) error {
	path := filepath.Join(projectDir, ".gitignore")
	wanted := []string{".ao/index.db", ".ao/logs/", ".ao/presence/", ".ao/outputs/"}

	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	var missing []string
	for _, w := range wanted {
		if !containsLine(existing, w) {
			missing = append(missing, w)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	content := existing
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(missing, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func containsLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// ClaudeDesktopSnippet returns the ready-to-paste Claude Desktop config for
// this machine, with the absolute path of the current ao binary filled in.
func ClaudeDesktopSnippet() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "/absolute/path/to/ao"
	}
	return fmt.Sprintf(`{
  "mcpServers": {
    "agentorchestra": {
      "command": %q,
      "args": ["mcp"]
    }
  }
}`, exe)
}
