package prompts

// The tmux layer fits the package as it is, and the Service answers the prompt half of the terminal's bridge
// (internal/terminal.Bridge: ClaimPrompt, PromptEnter; ReconcileCwd belongs to the layout).
var (
	_ Panes = TmuxPanes{}
	_ Clock = SystemClock{}
	_ interface {
		ClaimPrompt(id, session, text string) bool
		PromptEnter(session string)
	} = (*Service)(nil)
)
