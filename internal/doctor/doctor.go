// Package doctor runs read-only health checks over an AgentOrchestra
// installation and a project workspace, reporting each check with a fix
// hint. It never repairs anything itself — auto-fixes are how surprises
// happen; doctor only diagnoses.
package doctor

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/memory"
	"github.com/ponthepmk/AgentOrchestra/internal/presence"
	"github.com/ponthepmk/AgentOrchestra/internal/registry"
	"github.com/ponthepmk/AgentOrchestra/internal/store"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

// workerProbeTimeout keeps `ao doctor --worker-url` from hanging when the
// model server is down.
const workerProbeTimeout = 2 * time.Second

// Check is one diagnostic result.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"` // shown only when !OK
}

// Run executes all checks for the project at dir. workerURL is optional —
// when non-empty, the worker's model server is probed too.
func Run(dir, workerURL string) []Check {
	var checks []Check
	add := func(name string, ok bool, detail, fix string) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: detail, Fix: fix})
	}

	// 1. Binary reachable for agents that spawn `ao mcp` from PATH.
	if p, err := exec.LookPath("ao"); err == nil {
		add("ao in PATH", true, p, "")
	} else {
		add("ao in PATH", false, "not found",
			"copy the binary into PATH, e.g. `cp bin/ao /usr/local/bin/ao` — .mcp.json entries that say \"command\": \"ao\" need this")
	}

	// 2. Registry readable and default project points somewhere real.
	if f, err := registry.Load(); err != nil {
		add("project registry", false, err.Error(), "fix or delete the registry file, then re-run `ao init` in your project")
	} else if len(f.Projects) == 0 {
		add("project registry", false, "no projects registered", "run `ao init --id <project>` in a project directory")
	} else {
		detail := fmt.Sprintf("%d project(s), default: %s", len(f.Projects), f.Default)
		if defDir, ok := f.Projects[f.Default]; ok {
			if _, err := os.Stat(defDir); err != nil {
				add("project registry", false, detail+" — default dir missing: "+defDir,
					"the default project's directory no longer exists; re-run `ao init` there or edit the registry file")
			} else {
				add("project registry", true, detail, "")
			}
		} else {
			add("project registry", true, detail, "")
		}
	}

	// Project-level checks.
	ws := workspace.Open(dir)
	if !ws.Initialized() {
		add("workspace", false, dir+" is not initialized", "run `ao init --id <project>` in this directory")
		return checks // everything below depends on an initialized workspace
	}
	st, err := ws.ReadState()
	if err != nil {
		add("state (.ao/state.json)", false, err.Error(), "state file is corrupt; restore it from git or re-run `ao init`")
		return checks
	}
	add("state (.ao/state.json)", true,
		fmt.Sprintf("project %s, stage %s, holder %s", st.ProjectID, st.CurrentStage, st.HolderAgent), "")

	cfg, err := ws.LoadAgentConfig()
	switch {
	case err != nil:
		add("team (.agentconfig)", false, err.Error(), "fix the YAML syntax in .agentconfig")
	case len(cfg.Agents) == 0:
		add("team (.agentconfig)", false, "no agents configured", "add agents to .agentconfig or re-run `ao init` (team preset)")
	default:
		add("team (.agentconfig)", true,
			fmt.Sprintf("%d agents, stages: %s", len(cfg.Agents), strings.Join(cfg.Stages, "→")), "")
	}

	checks = append(checks, checkMCPJSON(dir))
	checks = append(checks, checkClaudeMD(dir))
	checks = append(checks, checkIndex(dir, st.ProjectID, ws))

	// Presence + memory are informational (always OK).
	online := onlineAgents(dir, cfg)
	add("presence", true, fmt.Sprintf("online now: %s", orNone(online)), "")
	add("memory", true, fmt.Sprintf("%d shared entries (.ao/memory/)", memory.Count(dir)), "")

	if workerURL != "" {
		checks = append(checks, checkWorker(workerURL))
	}
	return checks
}

// AllOK reports whether every check passed.
func AllOK(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

func checkMCPJSON(dir string) Check {
	name := "MCP config (.mcp.json)"
	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		return Check{Name: name, OK: false, Detail: "missing",
			Fix: "re-run `ao init` (it generates .mcp.json), or add an agentorchestra entry by hand"}
	}
	if !strings.Contains(string(data), "agentorchestra") {
		return Check{Name: name, OK: false, Detail: "exists but has no agentorchestra server entry",
			Fix: "add {\"agentorchestra\": {\"command\": \"ao\", \"args\": [\"mcp\"]}} under mcpServers"}
	}
	return Check{Name: name, OK: true, Detail: "agentorchestra server configured"}
}

func checkClaudeMD(dir string) Check {
	name := "instructions (CLAUDE.md)"
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(data), "agentorchestra:begin") {
		return Check{Name: name, OK: false, Detail: "AgentOrchestra section not found",
			Fix: "re-run `ao init` to (re)generate the instruction section — without it agents won't call ao_* tools on their own"}
	}
	return Check{Name: name, OK: true, Detail: "AgentOrchestra section present"}
}

func checkIndex(dir, projectID string, ws *workspace.Workspace) Check {
	name := "index (.ao/index.db)"
	idx, err := store.OpenSQLite(filepath.Join(dir, ".ao", "index.db"))
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error(), Fix: "delete .ao/index.db and run `ao log --reindex`"}
	}
	defer idx.Close()
	records, err := idx.ListHandoffs(projectID, store.ListFilter{})
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error(), Fix: "run `ao log --reindex`"}
	}
	files, err := ws.ListHandoffs()
	if err != nil {
		return Check{Name: name, OK: false, Detail: "cannot list .ao/handoffs: " + err.Error(), Fix: "check directory permissions"}
	}
	if len(records) != len(files) {
		return Check{Name: name, OK: false,
			Detail: fmt.Sprintf("index has %d handoffs but %d files exist", len(records), len(files)),
			Fix:    "run `ao log --reindex` to rebuild the index from files"}
	}
	return Check{Name: name, OK: true, Detail: fmt.Sprintf("%d handoffs indexed, matches files", len(records))}
}

func checkWorker(url string) Check {
	name := "worker model server"
	client := &http.Client{Timeout: workerProbeTimeout}
	base := strings.TrimSuffix(url, "/")
	// GET /models is the cheapest OpenAI-compatible probe; any HTTP response
	// (even 404) proves the server is up. Only transport errors fail.
	resp, err := client.Get(base + "/models")
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error(),
			Fix: "start the model server (Ollama/LM Studio/llama.cpp/vLLM) or fix --worker-url"}
	}
	resp.Body.Close()
	return Check{Name: name, OK: true, Detail: fmt.Sprintf("%s reachable (HTTP %d)", base, resp.StatusCode)}
}

func onlineAgents(dir string, cfg workspace.AgentConfig) []string {
	var online []string
	for _, a := range cfg.Agents {
		if presence.Get(dir, a.Name).Online() {
			online = append(online, a.Name)
		}
	}
	return online
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "(none)"
	}
	return strings.Join(list, ", ")
}
