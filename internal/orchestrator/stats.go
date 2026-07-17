package orchestrator

import (
	"fmt"
	"sort"
	"time"
)

// quickReturnWindow: a handoff bounced straight back to its sender within
// this window is counted as a "quick return". This is presented as raw
// data, not a failure verdict — some workflows legitimately ping-pong.
const quickReturnWindow = 30 * time.Minute

// AgentStats is one agent's handoff activity.
type AgentStats struct {
	Agent          string  `json:"agent"`
	Sent           int     `json:"sent"`
	Received       int     `json:"received"`
	AvgHoldMinutes float64 `json:"avg_hold_minutes"` // avg time from receiving the baton to passing it on; 0 when never measured
}

// Stats summarizes a project's handoff history.
type ProjectStats struct {
	ProjectID     string         `json:"project_id"`
	TotalHandoffs int            `json:"total_handoffs"`
	PerAgent      []AgentStats   `json:"per_agent"`
	PerStage      map[string]int `json:"per_stage"`
	QuickReturns  int            `json:"quick_returns"` // bounced back to sender within 30m (raw signal, not a verdict)
}

// Stats computes activity numbers from the handoff index.
func (o *Orchestrator) Stats() (ProjectStats, error) {
	if !o.ws.Initialized() {
		return ProjectStats{}, fmt.Errorf("workspace not initialized: run `ao init` in %s first", o.dir)
	}
	cfg, err := o.ws.LoadAgentConfig()
	if err != nil {
		return ProjectStats{}, err
	}
	records, err := o.Log(LogFilter{}) // most recent first
	if err != nil {
		return ProjectStats{}, err
	}
	// Work in chronological order.
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	stats := ProjectStats{
		ProjectID:     cfg.ProjectID,
		TotalHandoffs: len(records),
		PerStage:      map[string]int{},
	}

	sent := map[string]int{}
	received := map[string]int{}
	holdTotal := map[string]time.Duration{}
	holdCount := map[string]int{}

	for i, r := range records {
		sent[r.SourceAgent]++
		received[r.TargetAgent]++
		stats.PerStage[r.Stage]++

		if i+1 < len(records) {
			next := records[i+1]
			dt := next.CreatedAt.Sub(r.CreatedAt)
			// The receiver "held" the baton until they sent the next handoff.
			if next.SourceAgent == r.TargetAgent && dt >= 0 {
				holdTotal[r.TargetAgent] += dt
				holdCount[r.TargetAgent]++
				if next.TargetAgent == r.SourceAgent && dt < quickReturnWindow {
					stats.QuickReturns++
				}
			}
		}
	}

	names := map[string]bool{}
	for _, a := range cfg.Agents {
		names[a.Name] = true
	}
	for a := range sent {
		names[a] = true
	}
	for a := range received {
		names[a] = true
	}

	for name := range names {
		as := AgentStats{Agent: name, Sent: sent[name], Received: received[name]}
		if holdCount[name] > 0 {
			as.AvgHoldMinutes = holdTotal[name].Minutes() / float64(holdCount[name])
		}
		stats.PerAgent = append(stats.PerAgent, as)
	}
	sort.Slice(stats.PerAgent, func(i, j int) bool {
		if stats.PerAgent[i].Sent+stats.PerAgent[i].Received != stats.PerAgent[j].Sent+stats.PerAgent[j].Received {
			return stats.PerAgent[i].Sent+stats.PerAgent[i].Received > stats.PerAgent[j].Sent+stats.PerAgent[j].Received
		}
		return stats.PerAgent[i].Agent < stats.PerAgent[j].Agent
	})
	return stats, nil
}
