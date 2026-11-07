package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// One answer of a model for an app (the right `ai`): Claude Code in print mode with no tools, no settings and
// no session, in an empty folder, on the person's own login. The call is made only when its worst price fits in what
// is left of the app's budget of the day; it is capped twice (`--max-budget-usd` and the output's tokens), and the day
// is charged what the model says it cost.

// The bounds of a call.
const (
	AIPromptMax       = 20000 // characters
	AISystemMax       = 4000
	AIMaxTokens       = 8192
	AIDefaultTokens   = 1024
	aiOverheadTokens  = 2000 // what Claude Code adds to the prompt, with room to spare
	aiTimeout         = 2 * time.Minute
	aiCharsPerTokenLo = 2 // a token is seldom less: the estimate of the prompt's tokens is high, not low
)

// aiModels are the models an app may ask for, with prices per token above what they cost (for the worst price of a
// call, never for the bill: the day is charged what the model reports).
var aiModels = map[string]struct {
	Alias           string
	InCeil, OutCeil float64
}{
	"fast":  {Alias: "haiku", InCeil: 2e-6, OutCeil: 10e-6},
	"smart": {Alias: "sonnet", InCeil: 6e-6, OutCeil: 30e-6},
}

// AIRequest is what a view asks of the model.
type AIRequest struct {
	Prompt    string `json:"prompt"`
	System    string `json:"system,omitempty"`
	MaxTokens int    `json:"maxTokens,omitempty"`
	// Model is fast (the default) or smart.
	Model string `json:"model,omitempty"`
}

// AIAnswer is the model's answer and what it cost.
type AIAnswer struct {
	Text string  `json:"text"`
	USD  float64 `json:"usd"`
}

// Check fills the defaults and refuses what is out of bounds.
func (q *AIRequest) Check() error {
	if q.Model == "" {
		q.Model = "fast"
	}
	if _, ok := aiModels[q.Model]; !ok {
		return &BridgeError{Code: CodeInvalidParams, Message: "model is fast or smart"}
	}
	if strings.TrimSpace(q.Prompt) == "" || utf8.RuneCountInString(q.Prompt) > AIPromptMax {
		return &BridgeError{Code: CodeInvalidParams, Message: fmt.Sprintf("prompt is 1 to %d characters", AIPromptMax)}
	}
	if utf8.RuneCountInString(q.System) > AISystemMax {
		return &BridgeError{Code: CodeInvalidParams, Message: fmt.Sprintf("system is up to %d characters", AISystemMax)}
	}
	if q.MaxTokens == 0 {
		q.MaxTokens = AIDefaultTokens
	}
	if q.MaxTokens < 1 || q.MaxTokens > AIMaxTokens {
		return &BridgeError{Code: CodeInvalidParams, Message: fmt.Sprintf("maxTokens is 1 to %d", AIMaxTokens)}
	}
	return nil
}

// Worst is the most the call can cost.
func (q AIRequest) Worst() float64 {
	m := aiModels[q.Model]
	in := (len(q.Prompt)+len(q.System))/aiCharsPerTokenLo + aiOverheadTokens
	return float64(in)*m.InCeil + float64(q.MaxTokens)*m.OutCeil
}

// OverBudget is the refusal of a call the day's budget cannot pay for: what is spent, and what the call may cost.
func OverBudget(b Budget, spent, worst float64) error {
	return &BridgeError{Code: CodeQuotaExceeded, Message: fmt.Sprintf(
		"the app's budget of the day does not cover this call: %s of %s spent, the call may cost up to %s; it renews at 00:00 UTC",
		Money(spent), Money(b.USDPerDay), Money(worst))}
}

// AIRun runs the model's program; tests put their own.
type AIRun func(ctx context.Context, dir string, args, env []string, stdin string) ([]byte, error)

// RunClaude is AIRun over the claude program.
func RunClaude(bin string) AIRun {
	return func(ctx context.Context, dir string, args, env []string, stdin string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir, cmd.Env, cmd.Stdin = dir, env, strings.NewReader(stdin)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			msg := strings.TrimSpace(stderr.String())
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return nil, fmt.Errorf("%v: %s", err, msg)
		}
		return out, nil
	}
}

// CompleteAI asks the model once, spending at most capUSD.
func CompleteAI(ctx context.Context, run AIRun, q AIRequest, capUSD float64) (AIAnswer, error) {
	dir, err := os.MkdirTemp("", "hl-app-ai-")
	if err != nil {
		return AIAnswer{}, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, aiTimeout)
	defer cancel()
	// No tools, no MCP servers, no settings but the empty folder's: the model only answers.
	args := []string{"-p", "--output-format", "json", "--no-session-persistence", "--setting-sources", "project",
		"--strict-mcp-config", "--tools", "", "--model", aiModels[q.Model].Alias, "--max-budget-usd", strconv.FormatFloat(capUSD, 'f', 4, 64)}
	if q.System != "" {
		args = append(args, "--system-prompt", q.System)
	}
	env := append(aiEnv(os.Environ()), "CLAUDE_CODE_MAX_OUTPUT_TOKENS="+strconv.Itoa(q.MaxTokens))
	out, err := run(ctx, dir, args, env, q.Prompt)
	if err != nil {
		return AIAnswer{}, &BridgeError{Code: CodeHostUnavailable, Message: "the model did not answer", Detail: err.Error()}
	}
	var res struct {
		Result  string  `json:"result"`
		IsError bool    `json:"is_error"`
		Subtype string  `json:"subtype"`
		Cost    float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(lastJSONLine(out), &res); err != nil {
		return AIAnswer{}, &BridgeError{Code: CodeHostUnavailable, Message: "the model's answer could not be read", Detail: err.Error()}
	}
	if res.IsError {
		// It cost what it cost even so: the caller charges USD.
		return AIAnswer{USD: res.Cost}, &BridgeError{Code: CodeUpstreamError, Message: "the model refused: " + clipHookText(res.Subtype+" "+res.Result, 300)}
	}
	return AIAnswer{Text: res.Result, USD: res.Cost}, nil
}

// aiEnv is what of the runtime's environment the model's program gets: where it lives and how it signs in, nothing of
// the runtime's own secrets.
func aiEnv(all []string) []string {
	keep := []string{"PATH=", "HOME=", "USER=", "LOGNAME=", "LANG=", "LC_ALL=", "TMPDIR=", "XDG_CONFIG_HOME=", "XDG_DATA_HOME=",
		"HTTP_PROXY=", "HTTPS_PROXY=", "NO_PROXY=", "http_proxy=", "https_proxy=", "no_proxy=", "SSL_CERT_FILE=", "NODE_EXTRA_CA_CERTS=",
		"ANTHROPIC_", "CLAUDE_CONFIG_DIR=", "CLAUDE_CODE_USE_", "AWS_REGION=", "AWS_PROFILE=", "CLOUD_ML_REGION=", "ANTHROPIC_VERTEX_"}
	var out []string
	for _, kv := range all {
		for _, k := range keep {
			if strings.HasPrefix(kv, k) {
				out = append(out, kv)
				break
			}
		}
	}
	return out
}

// lastJSONLine is the last line of the output that holds an object (print mode may say something before it).
func lastJSONLine(out []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if l := bytes.TrimSpace(lines[i]); len(l) > 0 && l[0] == '{' {
			return l
		}
	}
	return out
}

// ChargeAI reserves the call's worst price within the budget, makes the call and charges the day what it cost.
func (r *Registry) ChargeAI(ctx context.Context, id string, b Budget, run AIRun, q AIRequest, now func() time.Time) (AIAnswer, Spent, error) {
	worst := q.Worst()
	got, err := r.ReserveUSD(id, b, worst, now())
	if err != nil {
		spent := r.SpentToday(id, now())
		return AIAnswer{}, spent, OverBudget(b, spent.USD, worst)
	}
	if got+1e-9 < worst {
		_ = r.SettleUSD(id, got, 0, now())
		// What is left cannot pay for the worst of this call: it is not made.
		spent := r.SpentToday(id, now())
		return AIAnswer{}, spent, OverBudget(b, spent.USD, worst)
	}
	ans, err := CompleteAI(ctx, run, q, got)
	cost := ans.USD
	if err != nil && cost == 0 {
		// The model's price is not known (no answer, or one that cannot be read): the day keeps the whole reservation.
		cost = got
	}
	_ = r.SettleUSD(id, got, cost, now())
	return ans, r.SpentToday(id, now()), err
}
