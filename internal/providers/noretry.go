package providers

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/terminalfailure"
)

// noRetry tells the agent's CLI not to send again a call the vendor refused (401, 403, 400 and the like): the
// Anthropic SDK reads X-Should-Retry before its own rules, so the turn fails in seconds instead of minutes of retries.
// Throttles, the vendor's own errors and no answer at all keep the CLI's retries.
func noRetry(w http.ResponseWriter, status int) {
	if retry, _ := terminalfailure.ClassifyStatus(status); retry == terminalfailure.RetryNever {
		w.Header().Set("X-Should-Retry", "false")
	}
}
