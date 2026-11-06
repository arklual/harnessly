package hubproto

// The keys a claude-settings item may set in ~/.claude/settings.json, and the variables it may put in its "env". Both
// the hub (when the item is saved) and the machine (when it is applied) hold to them: a list item never writes hooks,
// permissions, an API address or a key — hooks and MCP servers come in plugins, everything else stays the person's.
var (
	ClaudeSettingsKeys = []string{
		"env", "model", "effortLevel", "includeCoAuthoredBy", "cleanupPeriodDays", "subagentPromptCacheTtl",
		"alwaysThinkingEnabled", "outputStyle", "spinnerTipsEnabled",
	}
	ClaudeEnvKeys = []string{
		"CLAUDE_CODE_SUBAGENT_MODEL", "BASH_DEFAULT_TIMEOUT_MS", "BASH_MAX_TIMEOUT_MS", "CLAUDE_CODE_MAX_OUTPUT_TOKENS",
		"MAX_THINKING_TOKENS", "CLAUDE_CODE_DISABLE_FEEDBACK_SURVEY", "DISABLE_ERROR_REPORTING", "DISABLE_TELEMETRY",
		"DISABLE_AUTOUPDATER",
	}
)

// ClaudeMdMax is how long the CLAUDE.md block of a claude-settings item may be.
const ClaudeMdMax = 8 << 10
