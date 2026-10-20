/**
 * Model and reasoning effort chosen before a conversation starts.
 * Isomorphic: imported by API routes, launch builders and the welcome composer.
 */

export const AGENT_PROVIDERS = ['claude', 'codex'] as const;
export type TAgentProvider = (typeof AGENT_PROVIDERS)[number];

export const isAgentProvider = (value: unknown): value is TAgentProvider =>
  typeof value === 'string' && (AGENT_PROVIDERS as readonly string[]).includes(value);

/**
 * Levels accepted by `claude --effort` (claude 2.1.282 --help). `ultracode` (Claude Code 2.1.29x, `--effort ultracode`,
 * `/effort ultracode`) is xhigh with standing dynamic workflows: the agent may orchestrate subagents on its own. Claude
 * offers it on models with xhigh (Fable, Opus, Sonnet 5.x) and reports such a session's effort as `xhigh`.
 */
export const CLAUDE_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max', 'ultracode'] as const;
export const ULTRACODE_EFFORT = 'ultracode' as const;
/** The level Claude's status line reports while ultracode is on. */
export const ULTRACODE_REPORTED_LEVEL = 'xhigh' as const;
export type TClaudeEffort = (typeof CLAUDE_EFFORTS)[number];

/**
 * A model id is typed into a shell by tmux send-keys, so it must stay this strict:
 * aliases (`opus`), full ids (`claude-opus-5-5`, `gpt-5.4`) and the `[1m]` context suffix.
 */
export const MODEL_ID_RE = /^[a-z0-9][a-z0-9._-]{0,63}(\[1m\])?$/;

/** `null` means «Auto»: the flag is not passed and the CLI keeps its own default. */
export interface ILaunchChoice {
  model: string | null;
  effort: string | null;
}

export const AUTO_LAUNCH = { model: null, effort: null } as const satisfies ILaunchChoice;

export const isAutoLaunch = (choice: ILaunchChoice | null | undefined): boolean =>
  !choice || (choice.model === null && choice.effort === null);

/** Frozen answer of `POST /api/platform/switch-model {session, model}`: 200 with this body, or 400 | 404 | 409 with `{error}`. */
export interface ISwitchModelResponse {
  ok: true;
  /** Claude asked «Switch model?» and the web accepted it. */
  confirmed: boolean;
}
