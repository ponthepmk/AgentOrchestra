// Package notify sends best-effort desktop notifications so a human knows
// when the baton has moved to an agent that needs to be opened by hand.
// It never fails the caller: missing tools or unsupported platforms are
// silently ignored (the event is still in .ao/logs/ao.log).
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Send shows a desktop notification with the given title and body.
// Returns whether a notification was actually attempted.
func Send(title, body string) bool {
	switch runtime.GOOS {
	case "linux":
		if path, err := exec.LookPath("notify-send"); err == nil {
			return exec.Command(path, title, body).Run() == nil
		}
	case "darwin":
		if path, err := exec.LookPath("osascript"); err == nil {
			script := fmt.Sprintf("display notification %q with title %q", body, title)
			return exec.Command(path, "-e", script).Run() == nil
		}
	}
	return false
}
