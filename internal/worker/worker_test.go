package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponthepmk/AgentOrchestra/internal/orchestrator"
	"github.com/ponthepmk/AgentOrchestra/internal/workspace"
)

// mockLLM is an httptest server speaking just enough of the OpenAI chat
// completions API, capturing the last prompt it received.
func mockLLM(t *testing.T, reply string) (*httptest.Server, *string) {
	t.Helper()
	var lastPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, m := range req.Messages {
			if m.Role == "user" {
				lastPrompt = m.Content
			}
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastPrompt
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func setupProject(t *testing.T) (string, *orchestrator.Orchestrator) {
	t.Helper()
	t.Setenv("AO_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	orc := orchestrator.New(dir)
	agents := []workspace.Agent{
		{Name: "claude-code", Capabilities: []string{"coding"}},
		{Name: "ollama-worker", Capabilities: []string{"small-tasks"}},
	}
	if err := orc.Init("proj", agents, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return dir, orc
}

func TestWorkerExecutesTaskAndHandsBack(t *testing.T) {
	dir, orc := setupProject(t)
	srv, lastPrompt := mockLLM(t, "สรุปแล้ว: ทุกอย่างเรียบร้อย")

	// Give the worker an artifact to read.
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte("hello artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	_, err := orc.Handoff(orchestrator.HandoffRequest{
		SourceAgent: "claude-code",
		TargetAgent: "ollama-worker",
		Task:        "สรุปไฟล์ data.txt",
		Notes:       "เอาสั้นๆ พอ",
		Artifacts:   []string{"data.txt"},
	})
	if err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}

	w := New(Config{
		Dir:         dir,
		Agent:       "ollama-worker",
		BaseURL:     srv.URL + "/v1",
		Model:       "test-model",
		Poll:        50 * time.Millisecond,
		HandoffBack: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, quietLogger()) }()

	// Wait until the baton comes back to claude-code.
	deadline := time.After(5 * time.Second)
	for {
		st, err := orc.Status()
		if err == nil && st.HolderAgent == "claude-code" && strings.Contains(st.LastTask, "ollama-worker") {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("timed out waiting for worker to hand back; last status: %+v", st)
		case <-time.After(25 * time.Millisecond):
		}
	}
	cancel()
	<-done

	// The prompt must have included task, notes, and artifact content.
	for _, want := range []string{"สรุปไฟล์ data.txt", "เอาสั้นๆ พอ", "hello artifact"} {
		if !strings.Contains(*lastPrompt, want) {
			t.Errorf("expected prompt to contain %q, got: %s", want, *lastPrompt)
		}
	}

	// Output file exists and contains the model reply.
	outDir := filepath.Join(dir, ".ao", "outputs")
	entries, err := os.ReadDir(outDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected 1 output file, got %v (err %v)", entries, err)
	}
	data, _ := os.ReadFile(filepath.Join(outDir, entries[0].Name()))
	if !strings.Contains(string(data), "ทุกอย่างเรียบร้อย") {
		t.Fatalf("unexpected output content: %s", data)
	}
}

func TestWorkerDoesNotRepeatCompletedTask(t *testing.T) {
	dir, orc := setupProject(t)
	srv, _ := mockLLM(t, "done")

	_, err := orc.Handoff(orchestrator.HandoffRequest{
		SourceAgent: "claude-code",
		TargetAgent: "ollama-worker",
		Task:        "งานเดียวจบ",
	})
	if err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}

	// HandoffBack disabled: baton stays with the worker after completion,
	// which is exactly the re-execution trap the fingerprint must prevent.
	w := New(Config{
		Dir:     dir,
		Agent:   "ollama-worker",
		BaseURL: srv.URL + "/v1",
		Model:   "test-model",
		Poll:    30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx, quietLogger()) }()

	time.Sleep(400 * time.Millisecond) // several poll cycles
	cancel()
	<-done

	entries, err := os.ReadDir(filepath.Join(dir, ".ao", "outputs"))
	if err != nil {
		t.Fatalf("read outputs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 output despite multiple polls, got %d", len(entries))
	}
}

func TestWorkerSurvivesModelServerErrors(t *testing.T) {
	dir, orc := setupProject(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := orc.Handoff(orchestrator.HandoffRequest{
		SourceAgent: "claude-code",
		TargetAgent: "ollama-worker",
		Task:        "งานที่จะพัง",
	})
	if err != nil {
		t.Fatalf("Handoff failed: %v", err)
	}

	w := New(Config{
		Dir: dir, Agent: "ollama-worker",
		BaseURL: srv.URL + "/v1", Model: "test-model",
		Poll: 30 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := w.Run(ctx, quietLogger()); err != nil {
		t.Fatalf("worker must not crash on model errors, got: %v", err)
	}
}
