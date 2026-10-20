// Timeline entries as the runtime parses them from Claude Code and Codex transcripts (internal/transcript).
// Every entry has an id, a type and a timestamp in milliseconds; the rest depends on the type.

/** The agent's state in its terminal, as the runtime's status socket reports it. */
export type TCliState =
  'inactive' | 'idle' | 'busy' | 'needs-input' | 'ready-for-review' | 'cancelled' | 'unknown';

export type TToolStatus = 'pending' | 'success' | 'error';

interface IBase {
  id: string;
  timestamp: number;
}

export interface IImageRef {
  path?: string;
  url?: string;
  mediaType?: string;
  name?: string;
}

/** Files and images an agent sent to the person with its file-sending tool. */
export interface ISentFiles {
  /** Absolute paths, resolved against the agent's folder. */
  paths: string[];
  caption?: string;
  display?: string;
  /** Sent without being asked. */
  proactive?: boolean;
}

export interface ITimelineUserMessage extends IBase {
  type: 'user-message';
  text: string;
  /** Pictures sent with the message: data URLs or upload paths. */
  images?: string[];
  /** Typed while the agent was busy; it reaches the agent after the current turn. */
  queued?: boolean;
  /** Shown before the transcript has it (the page's own optimistic entry). */
  pending?: boolean;
  /** A message from another agent session rather than from the person. */
  peer?: { from?: string; session?: string } | null;
  proactive?: boolean;
  /** The first prompt of a conversation, typed on the launch screen. */
  launch?: boolean;
  /** Stands in for images still uploading. */
  attachmentPlaceholder?: string;
  /** When the agent read it (its prompt hook fired). */
  deliveredAt?: number;
  /** When the page sent it. */
  sentAt?: number;
  /** When the page saw the prompt leave the input; `takenByAgent` once the agent picked it up. */
  takenAt?: number;
  takenByAgent?: boolean;
  traceId?: string;
}

export interface IUsage {
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
  [k: string]: unknown;
}

export interface ITimelineAssistantMessage extends IBase {
  type: 'assistant-message';
  markdown: string;
  usage?: IUsage;
  model?: string;
  stopReason?: string | null;
}

/** An edit the agent made with a single replacement (Edit) or a full write (Write: oldString empty). */
export interface IToolDiff {
  filePath: string;
  oldString: string;
  newString: string;
}

export interface ITimelineThinking extends IBase {
  type: 'thinking';
  thinking: string;
}

/** A message the agent sent to another agent session. */
export interface IPeerMessage {
  to: string;
  text: string;
}

export interface ITimelineToolCall extends IBase {
  type: 'tool-call';
  toolUseId: string;
  toolName: string;
  summary: string;
  filePath?: string;
  diff?: IToolDiff;
  status: TToolStatus;
  peerMessage?: IPeerMessage;
  sentFiles?: ISentFiles;
  input?: unknown;
}

export interface ITimelineToolResult extends IBase {
  type: 'tool-result';
  toolUseId: string;
  isError: boolean;
  summary: string;
  answers?: Record<string, string>;
}

export interface ITimelineAgentGroup extends IBase {
  type: 'agent-group';
  agentType: string;
  description: string;
  entryCount: number;
  entries: ITimelineEntry[];
}

export interface ITimelineCommandOutput extends IBase {
  type: 'command-output';
  output: string;
  isError?: boolean;
}

export interface ITimelineSlashCommand extends IBase {
  type: 'slash-command';
  name: string;
  args: string;
  origin?: 'transcript' | 'page';
  pending?: boolean;
  /** What a local command printed, folded into its entry. */
  output?: string;
  isError?: boolean;
}

export interface ITimelinePlan extends IBase {
  type: 'plan';
  toolUseId: string;
  markdown: string;
  filePath?: string;
  allowedPrompts?: unknown;
  status: 'pending' | 'success' | 'error' | 'rejected' | string;
}

export interface ITimelineTaskProgress extends IBase {
  type: 'task-progress';
  action: 'create' | 'update';
  taskId: string;
  toolUseId?: string;
  subject?: string;
  description?: string;
  status: TTaskStatus;
}

/** A task of the agent's checklist, folded from task-progress entries. */
export type TTaskStatus = 'pending' | 'in_progress' | 'completed' | 'blocked';

export interface ITaskItem {
  taskId: string;
  subject: string;
  description?: string;
  status: TTaskStatus;
}

export interface ITimelineTaskNotification extends IBase {
  type: 'task-notification';
  taskId: string;
  status: string;
  summary: string;
  result?: string;
  usage?: { totalTokens?: number; toolUses?: number; durationMs?: number };
}

export interface IAskUserQuestionOption {
  label: string;
  description: string;
}

export interface IAskUserQuestionItem {
  question: string;
  header: string;
  multiSelect: boolean;
  options: IAskUserQuestionOption[];
  /** Some option carries a preview (code or a mock-up): the agent's screen shows the menu differently. */
  preview: boolean;
}

export interface ITimelineAskUserQuestion extends IBase {
  type: 'ask-user-question';
  toolUseId: string;
  questions: IAskUserQuestionItem[];
  status: TToolStatus;
  answers?: Record<string, string>;
  /** The answer as the agent received it (one line). */
  answer?: string;
}

export interface ITimelineErrorNotice extends IBase {
  type: 'error-notice';
  severity: 'error' | 'warning' | 'info' | string;
  message: string;
  errorCode?: string;
}

export interface ITimelineContextCompacted extends IBase {
  type: 'context-compacted';
  beforeTokens?: number;
  afterTokens?: number;
}

export interface ITimelineInterrupt extends IBase {
  type: 'interrupt';
}

export interface ITimelineSessionExit extends IBase {
  type: 'session-exit';
}

export interface ITimelineTurnEnd extends IBase {
  type: 'turn-end';
}

/** Codex: an approval the agent waits for (a command or a patch). */
export interface ITimelineApprovalRequest extends IBase {
  type: 'approval-request';
  approvalKind: string;
  callId: string;
  command?: string;
  cwd?: string;
  patches?: unknown;
  permissions?: unknown;
  status: TToolStatus | string;
}

export interface ITimelineReasoningSummary extends IBase {
  type: 'reasoning-summary';
  summary: string[];
  hasEncryptedContent?: boolean;
}

export interface ITimelineWebSearch extends IBase {
  type: 'web-search';
  callId: string;
  query?: string;
  resultsSummary?: string;
  status: TToolStatus;
}

export interface ITimelineMcpToolCall extends IBase {
  type: 'mcp-tool-call';
  callId: string;
  server: string;
  tool: string;
  argumentsSummary?: string;
  resultSummary?: string;
  status: TToolStatus;
}

export interface ITimelinePatchApply extends IBase {
  type: 'patch-apply';
  callId: string;
  files: { path: string; status?: string; kind?: string }[];
  diff?: string;
  success?: boolean;
  status: TToolStatus;
}

export interface ITimelineExecCommandStream extends IBase {
  type: 'exec-command-stream';
  callId?: string;
  command: string;
  cwd?: string;
  stdout?: string;
  stderr?: string;
  exitCode?: number;
  status: TToolStatus;
}

export type ITimelineEntry =
  | ITimelineUserMessage
  | ITimelineAssistantMessage
  | ITimelineThinking
  | ITimelineToolCall
  | ITimelineToolResult
  | ITimelineAgentGroup
  | ITimelineCommandOutput
  | ITimelineSlashCommand
  | ITimelinePlan
  | ITimelineTaskProgress
  | ITimelineTaskNotification
  | ITimelineAskUserQuestion
  | ITimelineErrorNotice
  | ITimelineContextCompacted
  | ITimelineInterrupt
  | ITimelineSessionExit
  | ITimelineTurnEnd
  | ITimelineApprovalRequest
  | ITimelineReasoningSummary
  | ITimelineWebSearch
  | ITimelineMcpToolCall
  | ITimelinePatchApply
  | ITimelineExecCommandStream;

/** How the page shows a message it sent before the transcript has it. */
export interface IPendingMessageOptions {
  images?: string[];
  /** Hide the pending entry once the transcript has the message. */
  autoHide?: boolean;
  queued?: boolean;
  launch?: boolean;
  attachmentPlaceholder?: string;
}

/** A slash command typed on the page, shown at once and replaced by the transcript's entry later. */
export const PENDING_SLASH_COMMAND_EVENT = 'hl:pending-slash-command';

export interface IPendingSlashCommandDetail {
  sessionName: string;
  name: string;
  args: string;
}
