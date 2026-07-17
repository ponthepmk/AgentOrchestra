// Package ui serves a read-only local dashboard for one project: current
// status, team presence, recent handoffs, shared memory, and stats — all in
// one page so nobody has to remember CLI subcommands to see what's going
// on. The page is embedded in the binary and works fully offline.
package ui

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/ponthepmk/AgentOrchestra/internal/memory"
	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
)

//go:embed index.html
var content embed.FS

// summary is everything the dashboard shows, fetched in one request.
type summary struct {
	Error    string                     `json:"error,omitempty"`
	Status   *orchestrator.Status       `json:"status,omitempty"`
	Agents   []orchestrator.AgentInfo   `json:"agents,omitempty"`
	Handoffs []handoffRow               `json:"handoffs,omitempty"`
	Memories []memory.Entry             `json:"memories,omitempty"`
	Stats    *orchestrator.ProjectStats `json:"stats,omitempty"`
}

type handoffRow struct {
	At     string `json:"at"`
	Source string `json:"source"`
	Target string `json:"target"`
	Stage  string `json:"stage"`
	Task   string `json:"task"`
}

// NewHandler returns the dashboard HTTP handler for the project at dir.
func NewHandler(dir string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(buildSummary(dir))
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page, _ := content.ReadFile("index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})

	return mux
}

func buildSummary(dir string) summary {
	o := orchestrator.New(dir)

	st, err := o.Status()
	if err != nil {
		return summary{Error: err.Error()}
	}
	out := summary{Status: &st}

	if agents, err := o.Agents(); err == nil {
		out.Agents = agents
	}
	if records, err := o.Log(orchestrator.LogFilter{Limit: 20}); err == nil {
		for _, r := range records {
			out.Handoffs = append(out.Handoffs, handoffRow{
				At:     r.CreatedAt.Format("2006-01-02 15:04"),
				Source: r.SourceAgent,
				Target: r.TargetAgent,
				Stage:  r.Stage,
				Task:   r.Task,
			})
		}
	}
	if mems, err := memory.List(dir, memory.Filter{}); err == nil {
		out.Memories = mems
	}
	if stats, err := o.Stats(); err == nil {
		out.Stats = &stats
	}
	return out
}
