// Package worker turns a local LLM server (Ollama, LM Studio, llama.cpp,
// vLLM — anything speaking the OpenAI-compatible chat completions API) into
// an AgentOrchestra team member: it polls the workspace for handoffs
// addressed to its agent name, runs the task against the model, writes the
// result to .ao/outputs/, and hands the baton back to the sender.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
	"github.com/ponthepmk/AgentOrchestra/internal/presence"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

// maxArtifactBytes caps how much artifact content goes into the prompt, so
// small local models aren't fed more context than they can handle.
const maxArtifactBytes = 32 * 1024

// Config tells a Worker who it is and which model server to drive.
type Config struct {
	Dir         string        // project root
	Agent       string        // agent name this worker acts as (e.g. "ollama-worker")
	BaseURL     string        // OpenAI-compatible base URL, e.g. http://localhost:11434/v1
	Model       string        // model to request
	APIKey      string        // optional bearer token
	Poll        time.Duration // how often to check for work; <= 0 means 5s
	HandoffBack bool          // hand the result back to the sender when done
}

// Worker is a polling loop bound to one workspace and one agent name.
type Worker struct {
	cfg  Config
	orc  *orchestrator.Orchestrator
	ws   *workspace.Workspace
	http *http.Client

	lastDone string // task fingerprint of the last handoff we completed
}

// New creates a Worker (does not start it — call Run).
func New(cfg Config) *Worker {
	if cfg.Poll <= 0 {
		cfg.Poll = 5 * time.Second
	}
	return &Worker{
		cfg:  cfg,
		orc:  orchestrator.New(cfg.Dir),
		ws:   workspace.Open(cfg.Dir),
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Run polls until ctx is canceled. Each cycle: heartbeat presence, check
// whether the baton is with our agent, and if so execute the task.
func (w *Worker) Run(ctx context.Context, log *slog.Logger) error {
	log.Info("worker started", "agent", w.cfg.Agent, "model", w.cfg.Model, "base_url", w.cfg.BaseURL, "poll", w.cfg.Poll.String())
	ticker := time.NewTicker(w.cfg.Poll)
	defer ticker.Stop()

	for {
		w.cycle(ctx, log)
		select {
		case <-ctx.Done():
			log.Info("worker stopped", "agent", w.cfg.Agent)
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Worker) cycle(ctx context.Context, log *slog.Logger) {
	if err := presence.Touch(w.cfg.Dir, w.cfg.Agent, "worker"); err != nil {
		log.Error("worker: presence touch failed", "error", err)
	}

	st, err := w.ws.ReadState()
	if err != nil {
		log.Error("worker: read state failed", "error", err)
		return
	}
	if st.HolderAgent != w.cfg.Agent {
		return // not our turn
	}
	fingerprint := st.UpdatedAt.String() + "|" + st.LastTask
	if fingerprint == w.lastDone {
		return // already handled this handoff; waiting for a new one
	}

	log.Info("worker: picked up task", "task", st.LastTask)
	handoff, err := w.latestHandoff()
	if err != nil {
		log.Error("worker: load latest handoff failed", "error", err)
		return
	}

	result, err := w.execute(ctx, handoff)
	if err != nil {
		log.Error("worker: model call failed (will retry next poll)", "error", err)
		return
	}

	outPath, err := w.writeOutput(result)
	if err != nil {
		log.Error("worker: write output failed", "error", err)
		return
	}
	log.Info("worker: task complete", "output", outPath)
	w.lastDone = fingerprint

	if w.cfg.HandoffBack && handoff != nil && handoff.SourceAgent != w.cfg.Agent {
		_, err := w.orc.Handoff(orchestrator.HandoffRequest{
			SourceAgent: w.cfg.Agent,
			TargetAgent: handoff.SourceAgent,
			Task:        fmt.Sprintf("รับผลงานจาก %s: %s", w.cfg.Agent, st.LastTask),
			Notes:       fmt.Sprintf("ผลลัพธ์เต็มอยู่ที่ `%s`", outPath),
			Artifacts:   []string{outPath},
		})
		if err != nil {
			log.Error("worker: handoff back failed", "error", err)
			return
		}
		log.Info("worker: handed result back", "to", handoff.SourceAgent)
	}
}

// latestHandoff returns the most recent handoff envelope, or nil if none.
func (w *Worker) latestHandoff() (*handoffView, error) {
	files, err := w.ws.ListHandoffs()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	env := files[len(files)-1].Envelope
	return &handoffView{
		SourceAgent: env.SourceAgent,
		Task:        env.Payload.Task,
		Notes:       env.Payload.Notes,
		Artifacts:   env.Payload.Artifacts,
	}, nil
}

type handoffView struct {
	SourceAgent string
	Task        string
	Notes       string
	Artifacts   []string
}

// execute builds the prompt and calls the model.
func (w *Worker) execute(ctx context.Context, h *handoffView) (string, error) {
	var prompt strings.Builder
	if h != nil {
		prompt.WriteString("Task: " + h.Task + "\n")
		if h.Notes != "" {
			prompt.WriteString("\nContext notes:\n" + h.Notes + "\n")
		}
		prompt.WriteString(w.artifactContext(h.Artifacts))
	}
	return w.chat(ctx, prompt.String())
}

// artifactContext inlines artifact file contents up to maxArtifactBytes.
func (w *Worker) artifactContext(artifacts []string) string {
	var b strings.Builder
	budget := maxArtifactBytes
	for _, rel := range artifacts {
		if budget <= 0 {
			b.WriteString("\n[more artifacts omitted: context budget reached]\n")
			break
		}
		data, err := os.ReadFile(filepath.Join(w.cfg.Dir, rel))
		if err != nil {
			continue
		}
		if len(data) > budget {
			data = data[:budget]
		}
		budget -= len(data)
		fmt.Fprintf(&b, "\n--- file: %s ---\n%s\n", rel, data)
	}
	return b.String()
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// chat calls POST {BaseURL}/chat/completions (OpenAI-compatible).
func (w *Worker) chat(ctx context.Context, prompt string) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model: w.cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a diligent worker agent in a multi-agent team. Complete the task directly and concisely. Answer in the language of the task."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}

	url := strings.TrimSuffix(w.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+w.cfg.APIKey)
	}

	resp, err := w.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call model server: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", fmt.Errorf("read model response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model server returned %s: %s", resp.Status, truncate(string(body), 300))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse model response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("model error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// writeOutput saves the model's result under .ao/outputs/ with the next
// sequence number, returning the path relative to the project root.
func (w *Worker) writeOutput(content string) (string, error) {
	outDir := filepath.Join(w.cfg.Dir, ".ao", "outputs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%03d-%s.md", len(entries)+1, strings.ReplaceAll(w.cfg.Agent, " ", "_"))
	if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	return filepath.Join(".ao", "outputs", name), nil
}
