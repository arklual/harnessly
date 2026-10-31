// The v2 protocol between a runtime and the page: the mirror of internal/wire (Go). internal/wire/schema/frames.json
// is the source of truth; the fixtures in internal/wire/testdata go through this file's parser in v2.test.ts and
// through the Go types in go test, and both check their closed lists against the schema.
//
// Times in *At fields are unix milliseconds by the machine's clock; the page shifts them by welcome.serverTime.

// ---- closed lists, in the schema's order

export const PROVIDERS = ['claude', 'codex'] as const;
export type Provider = (typeof PROVIDERS)[number];
export const FEATURES = [
  'queue',
  'steer',
  'questions',
  'permissions',
  'background',
  'model',
  'voice',
  'files',
  'checkpoints',
  'designly',
  'search',
  'vault',
  'push',
  'terminal',
  'machine',
  'apps',
  'settings',
  'drafts',
  'queue-move',
  'apps-hub',
  'terminal-stop',
  'rewind',
  'fork',
  'handoff',
  'auto-continue',
  'chrome',
] as const;
export type Feature = (typeof FEATURES)[number];
export const REACHES = ['online', 'unreachable', 'unknown'] as const;
export type Reach = (typeof REACHES)[number];
/** How a machine stands, worse later: warn is worth a look, critical is work about to fail. */
export const HEALTH_LEVELS = ['ok', 'warn', 'critical'] as const;
export type HealthLevel = (typeof HEALTH_LEVELS)[number];
export const HEALTH_METRICS = ['disk', 'mem', 'cpu'] as const;
export type HealthMetric = (typeof HEALTH_METRICS)[number];
export const AGENT_INSTALL_STATUSES = ['ready', 'missing', 'login-required'] as const;
export type AgentInstallStatus = (typeof AGENT_INSTALL_STATUSES)[number];
export const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'] as const;
export type Effort = (typeof EFFORTS)[number];
export const GOAL_STATUSES = ['active', 'paused', 'met'] as const;
export type GoalStatus = (typeof GOAL_STATUSES)[number];
export const TITLE_SOURCES = ['user', 'agent', 'first-message'] as const;
export type TitleSource = (typeof TITLE_SOURCES)[number];
/** Who stopped a chat's terminal: the person (terminal.stop) or the runtime, for standing idle. */
export const TERMINAL_STOP_BYS = ['person', 'idle'] as const;
export type TerminalStopBy = (typeof TERMINAL_STOP_BYS)[number];
export const TERMINAL_FAILURE_KINDS = ['quota_exhausted'] as const;
export type TerminalFailureKind = (typeof TERMINAL_FAILURE_KINDS)[number];
/** After how long without work the runtime stops a chat's terminal by itself. */
export const IDLE_STOPS = ['off', '1h', '2h', '6h'] as const;
export type IdleStop = (typeof IDLE_STOPS)[number];
/** What returning the folder to a snapshot does to a file: brings it back, changes it, or takes it away. */
export const FILE_CHANGES = ['restored', 'changed', 'removed'] as const;
export type FileChange = (typeof FILE_CHANGES)[number];
/** Where a conversation came from: a fork of another one, or a handoff of its context to another agent or model. */
export const ORIGIN_KINDS = ['fork', 'handoff'] as const;
export type OriginKind = (typeof ORIGIN_KINDS)[number];
/** How the context came over: the agent's own session copied, a budgeted excerpt of the turns, or a note the agent wrote. */
export const ORIGIN_VIAS = ['session', 'excerpt', 'note', 'history'] as const;
export type OriginVia = (typeof ORIGIN_VIAS)[number];
export const HANDOFF_MODES = ['excerpt', 'note', 'history'] as const;
export type HandoffMode = (typeof HANDOFF_MODES)[number];
/** What a draft is: an excerpt or a note for a handoff, or a fork's result to merge back. */
export const DRAFT_MODES = ['excerpt', 'note', 'result', 'history'] as const;
export type DraftMode = (typeof DRAFT_MODES)[number];
/** The longest text a handoff or a merge carries, in characters. */
export const HANDOFF_TEXT_MAX = 90_000;
/** A switch of the runtime's settings. */
export const ON_OFFS = ['on', 'off'] as const;
export type OnOff = (typeof ON_OFFS)[number];
/** Which usage limit an agent hit: the five-hour session, the weekly one, or one it did not name. */
export const LIMIT_WINDOWS = ['session', 'weekly', 'other'] as const;
export type LimitWindow = (typeof LIMIT_WINDOWS)[number];
/** Why the runtime went on by itself: the usage limit reset, or a restart cut a turn short. */
export const AUTO_CONTINUE_WHYS = ['limit', 'restart'] as const;
export type AutoContinueWhy = (typeof AUTO_CONTINUE_WHYS)[number];
/** A screen the person left open in the agent's terminal (their /login, a picker) that was closed for a message. */
export const TERMINAL_ASIDES = ['login', 'model', 'effort'] as const;
export type TerminalAside = (typeof TERMINAL_ASIDES)[number];
/** What `limit` asks of a chat held on its usage limit: go on now, do not go on by itself, go on by itself again. */
export const LIMIT_ACTIONS = ['now', 'cancel', 'auto'] as const;
export type LimitAction = (typeof LIMIT_ACTIONS)[number];
export const AGENT_STATE_KINDS = [
  'starting',
  'working',
  'waiting-question',
  'waiting-permission',
  'ready',
  'idle',
  'stopped',
  'launch-failed',
  'gone',
  'unknown',
] as const;
export type AgentStateKind = (typeof AGENT_STATE_KINDS)[number];
export const LAUNCH_STEPS = ['machine', 'cli', 'project', 'prompt'] as const;
export type LaunchStep = (typeof LAUNCH_STEPS)[number];
export const LAUNCH_FAILURES = [
  'no-process',
  'exited',
  'no-prompt-box',
  'no-session',
  'cli-missing',
  'login-required',
  'machine-busy',
] as const;
export type LaunchFailure = (typeof LAUNCH_FAILURES)[number];
export const GONE_REASONS = ['tab-closed', 'tmux-lost'] as const;
export type GoneReason = (typeof GONE_REASONS)[number];
export const TURN_STATUSES = ['live', 'done', 'interrupted', 'failed'] as const;
export type TurnStatus = (typeof TURN_STATUSES)[number];
export const TURN_FAILURE_CODES = [
  'api-error',
  'rate-limit',
  'context-overflow',
  'agent-exited',
  'unknown',
] as const;
export type TurnFailureCode = (typeof TURN_FAILURE_CODES)[number];
export const FILE_OPS = ['created', 'edited', 'read'] as const;
export type FileOp = (typeof FILE_OPS)[number];
/** What a turn made that the person opens: a published page, a design, a document, a merge request, a stand, a file handed over. */
export const OUTPUT_KINDS = ['artifact', 'design', 'doc', 'mr', 'stand', 'delivery'] as const;
export type OutputKind = (typeof OUTPUT_KINDS)[number];
export const OUTPUT_OPS = ['created', 'changed'] as const;
export type OutputOp = (typeof OUTPUT_OPS)[number];
/** ref is a link (artifact, mr, stand), a design's id, or a file's path (doc, delivery). */
export interface TurnOutput {
  kind: OutputKind;
  op: OutputOp;
  title: string;
  ref: string;
  /** A delivered file: its name, type, size in bytes and sha256 as the platform read them. */
  name?: string;
  mime?: string;
  size?: number;
  sha256?: string;
}
export const ENTRY_KINDS = [
  'user',
  'say',
  'think',
  'tool',
  'subagent',
  'answers',
  'gate',
  'compaction',
  'notice',
  'interrupt',
  'peer',
] as const;
export type EntryKind = (typeof ENTRY_KINDS)[number];
export const SUBAGENT_STATUSES = ['running', 'done', 'failed'] as const;
export type SubagentStatus = (typeof SUBAGENT_STATUSES)[number];
export const NOTICE_LEVELS = ['info', 'warn', 'error'] as const;
export type NoticeLevel = (typeof NOTICE_LEVELS)[number];
export const PEER_NOTE_KINDS = ['message', 'done', 'idle', 'event'] as const;
export type PeerNoteKind = (typeof PEER_NOTE_KINDS)[number];
export const ATTACHMENT_KINDS = ['image', 'file'] as const;
export type AttachmentKind = (typeof ATTACHMENT_KINDS)[number];
/** What a message's reference points at (MessageRef): nothing is uploaded, the runtime writes a line for it. */
export const REF_KINDS = ['design', 'doc', 'board', 'file', 'mr', 'task', 'chat'] as const;
export type RefKind = (typeof REF_KINDS)[number];
/** How many references one message or draft carries. */
export const REFS_MAX = 10;
export const MESSAGE_MODES = ['queue', 'now', 'steer'] as const;
export type MessageMode = (typeof MESSAGE_MODES)[number];
export const MESSAGE_KINDS = ['prompt', 'command'] as const;
export type MessageKind = (typeof MESSAGE_KINDS)[number];
export const MESSAGE_STATE_KINDS = [
  'queued',
  'typing',
  'in-input',
  'accepted',
  'in-turn',
  'done',
  'failed',
  'cancelled',
] as const;
export type MessageStateKind = (typeof MESSAGE_STATE_KINDS)[number];
export const QUEUE_REASONS = [
  'behind',
  'agent-busy',
  'launching',
  'resuming',
  'paused',
  'blocked-menu',
  'blocked-text',
  // Typed, and the agent did not take it in 30 s: back at the head of the queue, waiting for the person (caps=msgstatus).
  'stalled',
] as const;
export type QueueReason = (typeof QUEUE_REASONS)[number];
export const MESSAGE_FAILURES = [
  'upload-failed',
  'refused',
  'launch-failed',
  'agent-gone',
  'type-failed',
  'stuck-in-input',
  'unconfirmed',
  'expired',
  'returned-to-input',
] as const;
export type MessageFailure = (typeof MESSAGE_FAILURES)[number];
export const PAUSE_REASONS = [
  'interrupted',
  'user',
  'launch-failed',
  'agent-gone',
  'limited',
  'needs-target',
] as const;
export type PauseReason = (typeof PAUSE_REASONS)[number];
export const GATE_KINDS = ['question', 'permission', 'plan'] as const;
export type GateKind = (typeof GATE_KINDS)[number];
export const PERMISSION_KINDS = ['tool', 'plan'] as const;
export type PermissionKind = (typeof PERMISSION_KINDS)[number];
export const PERMISSION_SCOPES = ['once', 'always', 'deny', 'plan-auto', 'plan-ask', 'revise'] as const;
export type PermissionScope = (typeof PERMISSION_SCOPES)[number];
export const GATE_STATE_KINDS = ['open', 'answering', 'answered', 'closed'] as const;
export type GateStateKind = (typeof GATE_STATE_KINDS)[number];
export const GATE_CLOSE_REASONS = ['agent-moved-on', 'chat-about', 'turn-ended', 'agent-gone'] as const;
export type GateCloseReason = (typeof GATE_CLOSE_REASONS)[number];
/** Why a decision taken on the page did not reach the agent's menu: the person decides in the terminal. */
export const GATE_REFUSALS = ['menu-unmatched'] as const;
export type GateRefusal = (typeof GATE_REFUSALS)[number];
export const BACKGROUND_KINDS = ['subagent', 'command', 'workflow'] as const;
export type BackgroundKind = (typeof BACKGROUND_KINDS)[number];
export const BACKGROUND_STATUSES = ['running', 'done', 'stopped', 'failed', 'idle'] as const;
export type BackgroundStatus = (typeof BACKGROUND_STATUSES)[number];
export const SNAPSHOT_MODES = ['replace', 'after'] as const;
export type SnapshotMode = (typeof SNAPSHOT_MODES)[number];
export const FRAME_ERROR_CODES = [
  'proto',
  'bad-channel',
  'not-found',
  'too-many-subs',
  'unauthorized',
] as const;
export type FrameErrorCode = (typeof FRAME_ERROR_CODES)[number];
export const CLIENT_FRAME_KINDS = ['hello', 'sub', 'unsub', 'ping'] as const;
/** The optional parts of the frames this page reads, named in its socket's address (?caps=…) and in the HTTP calls
 *  that carry them: the runtime sends them only to a page that names them, so a tab left on an older build never meets
 *  a field it refuses. Not in hello: a runtime older than the page refuses a field of hello it does not know. */
export const CLIENT_CAPS = ['delegation', 'workflows', 'profiles', 'msgstatus'] as const;
/** CLIENT_CAPS as a query: `caps=…`. */
export const CAPS_QUERY = `caps=${CLIENT_CAPS.join(',')}`;
export type ClientFrameKind = (typeof CLIENT_FRAME_KINDS)[number];
export const SERVER_FRAME_KINDS = ['welcome', 'snapshot', 'ev', 'pong', 'error'] as const;
export type ServerFrameKind = (typeof SERVER_FRAME_KINDS)[number];
export const MACHINE_EVENT_TYPES = ['machine_status', 'machine_load'] as const;
export type MachineEventType = (typeof MACHINE_EVENT_TYPES)[number];
export const CONVS_EVENT_TYPES = [
  'conversation_upsert',
  'conversation_state',
  'conversation_seen',
  'conversation_removed',
  'project_upsert',
  'project_removed',
  'favorites_upsert',
  'draft_upsert',
] as const;
export type ConvsEventType = (typeof CONVS_EVENT_TYPES)[number];
export const CONV_EVENT_TYPES = [
  'turn_started',
  'step',
  'entry_update',
  'entry_replace',
  'now',
  'question_asked',
  'permission_requested',
  'plan_proposed',
  'gate_updated',
  'compaction',
  'turn_finished',
  'turn_failed',
  'quota_exhausted',
  'queue_changed',
  'prompt_state',
  'message_upsert',
  'background_upsert',
  'background_removed',
  'shots_set',
  'pictures_set',
  'feed_reset',
] as const;
export type ConvEventType = (typeof CONV_EVENT_TYPES)[number];
export const COMPACTION_PHASES = ['started', 'finished'] as const;
export type CompactionPhase = (typeof COMPACTION_PHASES)[number];
export const OPS = [
  'send',
  'cancel',
  'retry',
  'promote',
  'answer',
  'decide',
  'interrupt',
  'queue.resume',
  'seen',
  'conversation.create',
  'conversation.rename',
  'conversation.pin',
  'conversation.move',
  'conversation.delete',
  'conversation.restore',
  'conversation.resume',
  'model',
  'bg.stop',
  'project.create',
  'project.update',
  'project.delete',
  'favorites.group.create',
  'favorites.group.rename',
  'favorites.group.delete',
  'favorites.group.move',
  'favorites.place',
  'goal',
  'conversation.retitle',
  'draft.set',
  'queue.move',
  'terminal.stop',
  'rewind',
  'rewind.undo',
  'conversation.fork',
  'conversation.handoff',
  'conversation.merge',
  'limit',
] as const;
export type Op = (typeof OPS)[number];
export const PROMOTE_TOS = ['now', 'steer'] as const;
export type PromoteTo = (typeof PROMOTE_TOS)[number];
/** What a project's deletion does to its chats: keep moves them out to the common list, delete deletes them too. */
export const PROJECT_CHATS = ['keep', 'delete'] as const;
export type ProjectChats = (typeof PROJECT_CHATS)[number];
export const RESULT_KINDS = ['accepted', 'queued', 'refused'] as const;
export type ResultKind = (typeof RESULT_KINDS)[number];
export const REFUSE_CODES = [
  'bad-request',
  'conflict',
  'unsupported',
  'forbidden',
  'not-found',
  'machine-required',
  'machine-offline',
  'agent-gone',
  'agent-working',
  'not-working',
  'queue-full',
  'text-too-long',
  'attachment-missing',
  'attachment-too-large',
  'attachment-type',
  'too-many-attachments',
  'too-late',
  'not-failed',
  'gate-closed',
  'gate-answered',
  'option-unknown',
  'rate-limited',
  'internal',
  'timeout',
] as const;
export type RefuseCode = (typeof REFUSE_CODES)[number];
/** The refusals a retry of the same command can turn into a success: they get «Повторить», the rest only «Отменить». */
export const RETRYABLE_REFUSE_CODES = ['machine-offline', 'rate-limited', 'internal', 'timeout'] as const;
export type RetryableRefuseCode = (typeof RETRYABLE_REFUSE_CODES)[number];

// ---- model

export type Ms = number;
export type MachineId = string;
export type ConvId = `tab-${string}`;
/** 'c:<jsonl uuid>#<k>' (Claude), 'x:<byte offset>#<k>' (Codex, lines without a uuid) or 'live:<turnId>'. */
export type EntryId = string;
/** The id of the user entry that opened a turn: a transcript entry id, never a live one. */
export type TurnId = string;
export type ClientId = string;
export type ConvChannel = `conv:${ConvId}`;
export type Channel = 'machine' | 'convs' | ConvChannel;

export interface Machine {
  id: MachineId; // the fleet id; standalone 'local'
  name: string;
  host: string;
  reach: Reach;
  lastSeenAt: Ms | null;
  runtime: RuntimeInfo | null; // null: the runtime did not answer /api/v2/hello
  agents: MachineAgent[];
  load?: MachineLoad; // only while subscribed with {load: true}
  /** The machine's own judgement of its disk, memory and CPU; absent where the source does not judge it. */
  health?: MachineHealth;
}
/** Level is the worst of issues; issues are worst first, then disk, mem, cpu, and empty when ok. */
export interface MachineHealth {
  level: HealthLevel;
  /** When the machine came to this level. */
  since: Ms;
  issues: HealthIssue[];
}
export interface HealthIssue {
  metric: HealthMetric;
  level: Exclude<HealthLevel, 'ok'>;
}
export interface RuntimeInfo {
  version: string;
  proto: number;
  features: Feature[];
}
export interface MachineAgent {
  provider: Provider;
  version: string | null;
  status: AgentInstallStatus;
  /** The models the CLI takes, the first being the one a new conversation offers; empty when the machine does not know them. */
  models: AgentModel[];
  /** A running conversation of this CLI takes the `model` command. */
  canSwitchModel: boolean;
  /** The vendor setups (Kimi, GLM, Grok) a conversation can start on through this CLI; absent where there are none,
   *  and for a page that does not name the `profiles` cap. Draw only the `configured` ones. */
  profiles?: ProfileOffer[];
}
/** A profile id: lower case, `^[a-z][a-z0-9-]{0,31}$`. The runtime owns the list (`kimi`, `glm`, `xai`…). */
export type ProfileId = string;
export const PROFILE_VENDORS = ['kimi', 'glm', 'grok'] as const;
export type ProfileVendor = (typeof PROFILE_VENDORS)[number];
/** A backend profile a machine can start a conversation on: another vendor's models behind this CLI. */
export interface ProfileOffer {
  id: ProfileId;
  label: string;
  vendor: ProfileVendor;
  /** A key is saved; a profile without one is not offered. */
  configured: boolean;
  /** The profile's own models (the vendor's ids); a conversation of the profile takes only these. */
  models: AgentModel[];
}
/** Whether a key is saved: `tail` is «…» and its last four characters, null while none is saved. The key never comes back. */
export interface ProfileKey {
  configured: boolean;
  tail: string | null;
}
/** One profile on the Settings screen (GET /api/v2/providers lists every preset of the runtime, a key saved or not).
 *  `via` is the CLI it runs through, `host` the vendor's host the requests go to (shown, never typed). */
export interface ProfileInfo {
  id: ProfileId;
  label: string;
  vendor: ProfileVendor;
  via: Provider;
  host: string;
  /** One line on the plan; empty when there is none. */
  hint: string;
  key: ProfileKey;
  models: AgentModel[];
  /** The vendor CLI's own sign-in on the machine (a subscription) the profile runs on when no key is saved; absent
   *  when there is none. */
  login?: ProfileLogin;
}
/** A sign-in of the vendor's CLI: whom it is of (an email). */
export interface ProfileLogin {
  account: string;
}
export interface ProfileList {
  profiles: ProfileInfo[];
}
/** The body of PUT /api/v2/providers/{id}/key: 8–512 printable characters without a space. */
export interface ProfileKeyEdit {
  key: string;
}
export const PROFILE_TEST_REASONS = [
  'no-key',
  'unauthorized',
  'forbidden',
  'rate-limited',
  'unreachable',
  'timeout',
  'rejected',
] as const;
export type ProfileTestReason = (typeof PROFILE_TEST_REASONS)[number];
/** POST /api/v2/providers/{id}/test: one minimal request to the vendor with the saved key. A pass has no reason, a
 *  failure has one; `status` is the vendor's HTTP code, null where it did not answer. */
export interface ProfileTest {
  ok: boolean;
  reason: ProfileTestReason | null;
  status: number | null;
  ms: number;
}
export const LOGIN_MODES = ['subscription', 'console', 'device'] as const;
export type LoginMode = (typeof LOGIN_MODES)[number];
export const LOGIN_STATES = [
  'starting',
  'awaiting',
  'checking',
  'done',
  'failed',
  'cancelled',
  'expired',
] as const;
export type LoginState = (typeof LOGIN_STATES)[number];
export const LOGIN_INPUTS = ['code', 'none'] as const;
export type LoginInput = (typeof LOGIN_INPUTS)[number];
export const LOGIN_REASONS = [
  'not-installed',
  'start-failed',
  'code-rejected',
  'refused',
  'timeout',
] as const;
export type LoginReason = (typeof LOGIN_REASONS)[number];
/** One sign-in of an agent CLI, driven by the runtime. `url` is the vendor's link; `userCode` the one-time code to type
 *  there (device mode); `input: 'code'` asks the page to paste back the code the vendor's site showed. */
export interface LoginFlow {
  id: string;
  provider: Provider;
  mode: LoginMode;
  state: LoginState;
  input: LoginInput;
  url: string | null;
  userCode: string | null;
  startedAt: number;
  expiresAt: number;
  reason: LoginReason | null;
}
/** Answer of the sign-in routes under /api/v2/agents/{provider}/login: null when none was started. */
export interface LoginStatus {
  flow: LoginFlow | null;
}
/** The body of POST /api/v2/agents/{provider}/login. */
export interface LoginStart {
  mode: LoginMode;
}
/** The body of POST /api/v2/agents/{provider}/login/code. */
export interface LoginCode {
  id: string;
  code: string;
}
/** PUT .../key (an API key to the CLI's stdin; the body is {@link ProfileKeyEdit}) and POST .../logout. */
export interface LoginOK {
  ok: boolean;
}
export interface AgentModel {
  id: string;
  label: string;
  /** One short line on what the model is for; empty when the CLI says nothing honest about it. */
  hint: string;
  /** The reasoning efforts the model takes, empty when it has no choice. */
  efforts: Effort[];
  /** The effort it runs on when none is asked for. */
  defaultEffort: Effort | null;
  /** The model takes Claude Code's ultracode mode; absent when it does not. */
  ultracode?: boolean;
}
export interface MachineLoad {
  at: Ms;
  cpu: number;
  memUsed: number;
  memTotal: number;
  diskFree: number;
  diskTotal: number;
  busyAgents: number;
}
export interface Project {
  id: string;
  machineId: MachineId;
  path: string;
  name: string;
  git: boolean;
  createdAt: Ms;
}
/** A named group inside the favorites; chats are the ids of its conversations in the order shown. */
export interface FavGroup {
  id: string;
  name: string;
  chats: ConvId[];
}
/**
 * How the pinned conversations stand: the groups in their order, then the conversations of none (loose). Whether a
 * chat is a favorite is its own `pinned`; the layout only says where it stands. A pinned chat the layout does not
 * name shows first among the loose, and an id of a chat that is not pinned is ignored.
 */
export interface Favorites {
  groups: FavGroup[];
  loose: ConvId[];
}
/** The key of the new-chat field's draft, which has no conversation yet. */
export const DRAFT_NEW = 'new' as const;
/** The longest draft text the machine keeps, in characters; a longer one stays on its device. */
export const DRAFT_TEXT_MAX = 20000;
/**
 * A composer's draft as the machine keeps it for every device: a register with a revision. A write names the revision
 * it makes (the current + 1) and is refused with conflict otherwise; a clear is a write of ''.
 */
export interface Draft {
  key: ConvId | typeof DRAFT_NEW;
  text: string;
  /** The files already uploaded for the field; absent when none (a clear after a send empties them with the text). */
  attachments?: AttachmentRef[];
  /** The places the field points at; absent when none. */
  refs?: MessageRef[];
  rev: number;
  deviceId: string;
  at: Ms;
}
/** An agent session on the machine that no conversation holds: the person may continue it. */
export interface AgentSession {
  provider: Provider;
  sessionId: string;
  title: string;
  cwd: string;
  updatedAt: Ms;
  turns: number;
}
/** One of a conversation's own terminals: a shell in its folder that the machine keeps until it is closed. */
export interface Terminal {
  id: string;
  createdAt: Ms;
}
export type DesignKind = 'site' | 'slides' | 'ds';
export const DESIGN_KINDS: readonly DesignKind[] = ['site', 'slides', 'ds'];
/** What a person may do in a project, weakest first. Absent from a runtime that does not restrict access. */
export type DesignRole = 'view' | 'comment' | 'edit' | 'manage' | 'owner';
export const DESIGN_ROLES: readonly DesignRole[] = ['view', 'comment', 'edit', 'manage', 'owner'];
/** How a deck's speaker notes are written. */
export type DesignDeckTone = 'talk' | 'biz' | 'short';
export const DESIGN_DECK_TONES: readonly DesignDeckTone[] = ['talk', 'biz', 'short'];
/** The company template a new deck takes its look from (POST /design/deck/pptx-style read it). */
export interface DesignlyDeckStyle {
  source: string;
  /** `--deck-*` CSS variables. */
  vars: Record<string, string>;
  layouts: number;
}
/** What a new deck is asked to be: minutes 1..180 or 0 (the agent decides), slides 2..40 or null for the agent's pick. */
export interface DesignlyDeck {
  minutes: number;
  slides: number | null;
  tone: DesignDeckTone;
  script: boolean;
  style: DesignlyDeckStyle | null;
}
/** The body of POST /api/v2/designly/create. */
export interface DesignlyCreate {
  title: string;
  brief: string;
  kind: DesignKind;
  provider: Provider;
  model: string | null;
  effort: Effort | null;
  designSystem: string | null;
  /** Only for the kind `slides`; an older runtime refuses the field, so it is sent only for a deck. */
  deck?: DesignlyDeck;
  /** Uploads (POST /api/v2/upload) the first message carries, at most 20. */
  attachmentIds?: string[];
}
/** The project made and the conversation opened in its folder. */
export interface DesignlyCreated {
  id: string;
  title: string;
  conv: string;
}
export interface DesignBoard {
  title: string;
  path: string;
}
/** Who made a project; `me` says it is the person asking. */
export interface DesignOwner {
  name: string;
  me: boolean;
}
/**
 * A project of the library; convs are the v2 conversations whose folder is the project's, touchedBy the others that
 * worked on it from any folder (their agent wrote its files or named it to the design CLI, read from the transcripts:
 * the chats from before count too), the latest activity first; an older runtime leaves it out. The owner is there for
 * a project made after owners were recorded: an older one has none.
 */
export interface DesignArtifact {
  id: string;
  title: string;
  updatedAt: Ms;
  kind: DesignKind;
  pinned: boolean;
  cover: string | null;
  projectPath: string;
  boards: DesignBoard[];
  convs: string[];
  touchedBy?: string[];
  owner?: DesignOwner;
  role?: DesignRole;
}
/** The library: the port of its server on the machine, its folder, its projects. */
export interface DesignLibrary {
  port: number;
  root: string;
  artifacts: DesignArtifact[];
}
export interface DesignSystemInfo {
  id: string;
  label: string;
  desc: string;
}
export interface DesignlyPin {
  id: string;
  pinned: boolean;
}
/** What a designly page answers when it refuses (status 4xx/5xx): projectId names a project made before the failure. */
export interface DesignlyRefusal {
  r: 'refused';
  code: RefuseCode;
  message: string;
  projectId: string | null;
}
export type TaskStatus = 'pending' | 'in_progress' | 'completed' | 'blocked';
export const TASK_STATUSES: readonly TaskStatus[] = ['pending', 'in_progress', 'completed', 'blocked'];
/** One line of the plan the agent keeps while it works (GET /api/v2/conversations/{id}/tasks), in the order made. */
export interface Task {
  id: string;
  subject: string;
  status: TaskStatus;
}
/** How full a conversation's agent window is (GET /api/v2/conversations/{id}/context): the tokens its last answer
 *  was asked with (0 before the first) of its window. */
export interface ConvContext {
  usedTokens: number;
  limitTokens: number;
}
/** What the runtime knows of a message by its clientId (GET /api/v2/conversations/{id}/messages?ids=a,b). */
export const MESSAGE_STATUS_KINDS = ['known', 'unknown', 'expired'] as const;
export type MessageStatusKind = (typeof MESSAGE_STATUS_KINDS)[number];
/** Where a message typed into the agent is: its input box, its own queue (a steer), with the agent, in the transcript. */
export const HANDED_WHERES = ['input-box', 'agent-queue', 'agent', 'transcript'] as const;
export type HandedWhere = (typeof HANDED_WHERES)[number];
export interface MessageHanded {
  at: Ms;
  where: HandedWhere;
  entryId: EntryId | null;
}
export type MessageStatus =
  | { clientId: ClientId; k: 'known'; message: Message; handed?: MessageHanded }
  | { clientId: ClientId; k: 'unknown' }
  | { clientId: ClientId; k: 'expired'; at: Ms };
export interface MessageStatuses {
  messages: MessageStatus[];
}
/** Who wrote a message the search found. */
export const MESSAGE_ROLES = ['user', 'agent'] as const;
export type MessageRole = (typeof MESSAGE_ROLES)[number];
/** One message found (GET /api/v2/search): its conversation, its entry, the turn the entry is in (null for one outside
 *  any), and the words around the match. */
export interface MessageHit {
  convId: ConvId;
  entryId: EntryId;
  turnId: TurnId | null;
  at: Ms;
  role: MessageRole;
  snippet: string;
}
/** The messages that hold every word of the query, the newest first. `indexing`: the runtime is still reading the
 *  older conversations, so the answer may lack some. */
export interface MessageSearch {
  query: string;
  hits: MessageHit[];
  indexing: boolean;
}
/** What a chat said, cut to the size of a context block (GET /api/v2/conversations/{id}/digest); `path` is the whole
 *  conversation as Markdown on the machine. */
export interface ChatDigest {
  convId: ConvId;
  title: string;
  text: string;
  path: string;
  truncated: boolean;
}
/** The parts of the disk layout, in the order they are drawn; `other` is the used part none of the named folders
 *  accounts for. */
export const DISK_PART_IDS = ['workspace', 'caches', 'agents', 'other', 'free'] as const;
export type DiskPartId = (typeof DISK_PART_IDS)[number];
export interface MachineDiskPart {
  id: DiskPartId;
  bytes: number;
}
/** The filesystem of the runtime user's home and what its used part is made of. The folders are measured in the
 *  background: `measuredAt` is null until the first walk ends (all that is used is then `other`), `partial` says the
 *  last walk ran out of time, so the named parts are lower bounds. */
export interface MachineDisk {
  path: string;
  total: number;
  free: number;
  parts: MachineDiskPart[];
  measuredAt: number | null;
  partial: boolean;
}
/** The machine at one moment of the last hour: the busy share of the processor and the share of memory in use (0…1),
 *  bytes per second over the network. */
export interface MachineSample {
  at: number;
  cpu: number;
  mem: number;
  rx: number;
  tx: number;
}
export const MACHINE_PROC_KINDS = ['service', 'agent', 'dev', 'other'] as const;
export type MachineProcKind = (typeof MACHINE_PROC_KINDS)[number];
/** A heavy process of the runtime's user. `pid` with `startTime` names it for its whole life; `cpu` is the share of
 *  one core like top (it passes 100 for a busy multi-threaded process); `guard`: shown, never stopped. */
export interface MachineProc {
  pid: number;
  startTime: string;
  name: string;
  command: string;
  cpu: number;
  rss: number;
  kind: MachineProcKind;
  port: number | null;
  stuck: boolean;
  guard: boolean;
}
/** GET /api/v2/machine/stats: the machine now (shares 0…1, bytes, bytes per second), its last hour thinned to a point
 *  every two minutes (oldest first, the last is now), the disk and the heaviest processes. */
export interface MachineStats {
  at: number;
  cpu: number;
  cores: number[];
  mem: { used: number; total: number };
  net: { rx: number; tx: number };
  disk: MachineDisk;
  history: MachineSample[];
  procs: MachineProc[];
}
/** POST /api/v2/machine/processes/stop: the process the page was shown. */
export interface MachineStop {
  pid: number;
  startTime: string;
}
export interface MachineStopped {
  stopped: true;
  signal: 'SIGTERM' | 'SIGKILL';
}
export const MACHINE_STOP_REASONS = [
  'gone',
  'changed',
  'foreign',
  'protected',
  'not-stoppable',
  'not-listed',
  'busy',
  'failed',
] as const;
export type MachineStopReason = (typeof MACHINE_STOP_REASONS)[number];
/** What a stop answers when it refuses: the protocol's code, why, and the runtime's own words (English). */
export interface MachineRefusal {
  r: 'refused';
  code: RefuseCode;
  reason: MachineStopReason;
  message: string;
}
/** The apps of the Apps screen, in the order of its tabs. */
export const APP_IDS = ['tracker', 'gitlab'] as const;
export type AppId = (typeof APP_IDS)[number];
/** `ready`: the sections are the app's answer; `off`: the machine is not set up for it; `failed`: it did not answer. */
export const APP_STATES = ['ready', 'off', 'failed'] as const;
export type AppState = (typeof APP_STATES)[number];
export const APP_REASONS = ['not-configured', 'no-token', 'denied', 'unreachable'] as const;
export type AppReason = (typeof APP_REASONS)[number];
export const APP_SECTION_IDS = ['epics', 'tasks', 'mrs'] as const;
export type AppSectionId = (typeof APP_SECTION_IDS)[number];
/** What a merge request waits for or is stuck on. */
export const APP_FLAGS = ['review', 'conflict', 'ci-failed', 'no-reviewer', 'open'] as const;
export type AppFlag = (typeof APP_FLAGS)[number];
export const APP_TONES = ['accent', 'warn'] as const;
export type AppTone = (typeof APP_TONES)[number];
/** How a row concerns the person who reads: they wrote it, it is on them, they are asked to review it. */
export const APP_ROLES = ['author', 'assignee', 'reviewer'] as const;
export type AppRole = (typeof APP_ROLES)[number];
/** The state of a merge request's latest pipeline. */
export const APP_PIPELINES = [
  'success',
  'failed',
  'running',
  'pending',
  'canceled',
  'skipped',
  'manual',
] as const;
export type AppPipeline = (typeof APP_PIPELINES)[number];
/** A task named from another: the epic a task belongs to. */
export interface AppRef {
  id: string;
  title: string;
  url: string;
}
/** A subtask of an epic. */
export interface AppChild {
  id: string;
  title: string;
  url: string;
  state: string | null;
  done: boolean;
  person: string | null;
}
/** What GitLab says of a merge request beyond what every row has. */
export interface AppMR {
  source: string | null;
  draft: boolean;
  conflicts: boolean;
  needsRebase: boolean;
  ready: boolean;
  unresolved: boolean;
  comments: number;
  pipeline: AppPipeline | null;
  approvals: { given: number; required: number } | null;
}
/** A task, an epic or a merge request of the Apps screen. `key` is unique on the screen, `id` is what the person reads
 *  (`ABC-2628`, `!502`); a task has the tracker's own `state` name (a merge request `opened`, `merged`, `closed`), an
 *  epic its `progress` and `children`, a merge request a `flag` and its `mr`. */
export interface AppRow {
  key: string;
  id: string;
  title: string;
  url: string;
  state: string | null;
  flag: AppFlag | null;
  tone: AppTone | null;
  project: string | null;
  person: string | null;
  target: string | null;
  createdAt: number;
  updatedAt: number;
  closedAt: number | null;
  author: string | null;
  assignees: string[];
  reviewers: string[];
  me: AppRole[];
  labels: string[];
  priority: string | null;
  kind: string | null;
  parent: AppRef | null;
  progress: { done: number; total: number } | null;
  children: AppChild[];
  mr: AppMR | null;
}
export interface AppSection {
  id: AppSectionId;
  rows: AppRow[];
}
/** One app's answer; `reason` is null exactly when it is `ready`, and an app that is not has no sections. `me` is how
 *  the service names the person whose token the machine reads with. */
export interface AppFeed {
  id: AppId;
  state: AppState;
  reason: AppReason | null;
  host: string | null;
  me: string | null;
  sections: AppSection[];
}
/** GET /api/v2/apps: every app of the screen; `staleDays` is how long a row stands still to be stale by default. */
export interface AppsFeed {
  at: number;
  staleDays: number;
  apps: AppFeed[];
}
/** The options of the agent CLIs' settings files (Claude Code's settings.json, Codex's config.toml), in the schema's
 *  order: `model`, `effort` and `baseUrl` are both CLIs', the rest belong to one. */
export const AGENT_OPTION_KEYS = [
  'model',
  'effort',
  'baseUrl',
  'fallbackModel',
  'thinking',
  'outputStyle',
  'permissionMode',
  'sandbox',
  'projectMcp',
  'compactWindow',
  'memory',
  'keepDays',
  'hooks',
  'coauthor',
  'updates',
  'summary',
  'fast',
  'sandboxMode',
  'network',
  'approval',
  'search',
  'history',
  'ultracode',
] as const;
export type AgentOptionKey = (typeof AGENT_OPTION_KEYS)[number];
/** An option's value: a name, a switch, a whole number, or null while the file does not set it. */
export type AgentOptionValue = string | boolean | number | null;
export interface AgentOption {
  key: AgentOptionKey;
  value: AgentOptionValue;
}
/** Claude Code's permission rules: `Bash(npm run *)`, `mcp__gitlab`. */
export interface AgentRules {
  allow: string[];
  ask: string[];
  deny: string[];
}
/** A variable of Claude Code's `env`; a secret's value is bullets and its last four characters. */
export interface AgentEnvVar {
  key: string;
  value: string;
  secret: boolean;
}
export const AGENT_MCP_KINDS = ['stdio', 'http'] as const;
export type AgentMcpKind = (typeof AGENT_MCP_KINDS)[number];
/** An MCP server the machine has set up for the CLI; `target` is the program's name or the address's host. */
export interface AgentMcp {
  name: string;
  kind: AgentMcpKind;
  target: string;
  enabled: boolean;
}
export const AGENT_ACCOUNT_METHODS = ['subscription', 'key'] as const;
export type AgentAccountMethod = (typeof AGENT_ACCOUNT_METHODS)[number];
/** How the CLI is signed in on this machine; never a token or a key. */
export interface AgentAccount {
  signedIn: boolean;
  method: AgentAccountMethod | null;
  plan: string | null;
  who: string | null;
}
/** GET and PATCH /api/v2/agent-settings/{provider}: every option the CLI takes (null while unset), Claude Code's rules
 *  (null for Codex) and environment (empty for Codex), the MCP servers and the sign-in. `rev` is the file's hash. */
export interface AgentSettings {
  provider: Provider;
  path: string;
  rev: string;
  account: AgentAccount;
  options: AgentOption[];
  rules: AgentRules | null;
  env: AgentEnvVar[];
  mcp: AgentMcp[];
}
/** An account an agent CLI was signed in with on one of the person's machines, as the cluster keeps it (the
 *  CLI forgets an account once it signs out). */
export interface AgentAccountNote {
  machineId: MachineId;
  provider: Provider;
  who: string;
  firstAt: Ms;
  lastAt: Ms;
}
/** What a write changes; what it does not name stays as it is. A null option value unsets the option. */
export interface AgentSettingsPatch {
  options?: AgentOption[];
  rules?: AgentRules;
  envSet?: { key: string; value: string }[];
  envUnset?: string[];
  mcp?: { name: string; enabled: boolean };
}
/** The Legacy tab a conversation runs in (GET /api/v2/conversations/{id}/legacy): what the pane's tools of the
 *  current interface read — its timeline, diff, merge requests, subagents, spend. */
export interface LegacyTab {
  tabId: string;
  workspaceId: string;
  sessionName: string;
  jsonlPath: string | null;
  agentSessionId: string | null;
  cwd: string;
}
/** The tab of the right pane a chip or a link opens in (GET /api/v2/conversations/{id}/open): a tracker task, a
 *  GitLab merge request or commit, a published document or board, any other site in the browser. */
export const OPEN_TABS = ['task', 'gitlab', 'artifacts', 'browser'] as const;
export type OpenTab = (typeof OPEN_TABS)[number];
export interface OpenTarget {
  tab: OpenTab;
  target: string;
  /** Present when the ref fits several places (`!12` in two projects): the best first, which is tab and target. */
  choices?: OpenChoice[];
}
/** One place a ref may open: the project, and what is known of the merge request there (its title). */
export interface OpenChoice {
  tab: OpenTab;
  target: string;
  label: string;
  detail: string | null;
}
/** Where each ref of GET /api/v2/conversations/{id}/open-refs opens, in the order asked; null: nowhere. */
export interface OpenedRefs {
  refs: { ref: string; open: OpenTarget | null }[];
}
export type FileKind = 'file' | 'directory';
export const FILE_KINDS: readonly FileKind[] = ['file', 'directory'];
/** One entry of a conversation's folder; path is from the folder's root. */
export interface FileEntry {
  name: string;
  path: string;
  kind: FileKind;
}
/** GET /api/v2/conversations/{id}/files?path=: folders first, then files, by name; truncated: the folder holds more. */
export interface DirListing {
  path: string;
  entries: FileEntry[];
  truncated: boolean;
}
/** GET /api/v2/conversations/{id}/file?path=: a UTF-8 text file of at most 1 MB, with the revision a save names. */
export interface FileContent {
  path: string;
  content: string;
  revision: string;
  bytes: number;
}
/** PUT /api/v2/conversations/{id}/file?path=: the whole new text over the revision that was read; answers the file as it is now. */
export interface FileSave {
  content: string;
  revision: string;
}
/** What a files page answers when it refuses: forbidden (outside the folder), not-found, unsupported (not text, over 1 MB). */
export interface FileRefusal {
  r: 'refused';
  code: RefuseCode;
  message: string;
}
/** What a recording said (POST /api/v2/voice); empty for silence. */
export interface Transcript {
  text: string;
}
/** A folder offered for a new project; branch is the checked out one of a git folder, null for any other. */
export interface Dir {
  path: string;
  name: string;
  git: boolean;
  branch: string | null;
  empty: boolean;
}
/**
 * A conversation's model. `ultracode` is Claude Code's mode beside the effort: standing dynamic workflows with
 * subagents on every task, at any effort; absent is off. A record of before that says effort "ultracode" is read as
 * xhigh with the mode on (what `--effort ultracode` ran).
 */
export interface Model {
  id: string;
  effort: Effort | null;
  ultracode?: boolean;
  /** The backend profile the chat runs on (ProfileOffer.id); absent for the CLI's own account. Set when the chat
   *  starts and never changed. A command names it beside the model (`profile`), not here. */
  profile?: ProfileId;
}
/** What the chats started by one prompt on several models share: the launch's id and the prompt (at most 200 characters). */
export interface Launch {
  id: string;
  prompt: string;
}
/** The agent's /goal: the condition it works toward; `since` is when it was set. */
export interface Goal {
  text: string;
  status: GoalStatus;
  since: Ms;
}
/** Claude's «while you were away» summary of the session; `at` is when it was written. */
export interface Recap {
  text: string;
  at: Ms;
}
/**
 * A model change accepted and not applied yet: the runtime applies it at the first moment the agent is at its prompt
 * (a turn over, an agent started), before the next queued message. `applying` is true while it is being typed in.
 */
export interface PendingModel {
  id: string;
  effort: Effort | null;
  ultracode?: boolean;
  since: Ms;
  applying: boolean;
}
/**
 * A change of model the runtime applied; the feed draws it where it happened. `from` is null when none was on record.
 * `refused`, when set, says the agent did not take the change `to` asked for, and why: the record stayed on `from`.
 */
export interface ModelSwitch {
  at: Ms;
  from: Model | null;
  to: Model;
  refused?: RefuseCode;
}
export interface Conversation {
  id: ConvId;
  machineId: MachineId;
  projectId: string | null;
  cwd: string;
  provider: Provider;
  model: Model | null;
  title: string;
  titleSource: TitleSource;
  pinned: boolean;
  createdAt: Ms;
  lastActivityAt: Ms;
  lastPromptAt: Ms; // the last real human message; createdAt until then — the sidebar orders by it
  state: AgentState;
  seenAt: Ms; // read up to; unread when lastTurn.endedAt > seenAt — not an agent state
  lastTurn: TurnSummary | null;
  queue: { count: number; paused: QueuePause | null };
  gate: { kind: GateKind; id: string } | null; // the open gate; its details are in the conv channel
  goal?: Goal; // the agent's /goal; absent when it has none
  launch?: Launch; // set on a chat started with others by one prompt on several models
  recap?: Recap; // Claude's latest recap; absent when it has none
  bgAgents?: number; // background work it waits for, all kinds together (subagents, commands, monitors, a wakeup); absent when none — the chat is at work, not done
  bgWait?: BgWait; // what bgAgents is made of; absent when it waits for nothing
  pendingModel?: PendingModel; // a model change that waits for the agent; absent when none does
  modelLog?: ModelSwitch[]; // the changes applied, oldest first (the last 20); absent when there were none
  terminalStop?: TerminalStop; // the terminal was stopped and the agent is not up again; absent otherwise
  terminalFailure?: TerminalFailure; // the last turn cannot be retried on this target; the person chooses another
  rewinds?: RewindMark[]; // the rewinds to an earlier message, oldest first (the last 10); absent when there were none
  origin?: Origin; // the conversation this one was forked or handed off from; absent for one started anew
  limited?: LimitHold; // the last turn ended on the usage limit and the chat waits for its reset; absent otherwise
  autoContinues?: AutoContinue[]; // the times the runtime went on by itself, oldest first (the last 20); absent when none
  parentId?: ConvId; // the chat whose agent delegated this one; absent for a chat the person started
  rev: number;
}
/**
 * A chat held on its agent's usage limit. `resetsAt` is when the limit resets (null: unknown); `continueAt` is when the
 * runtime goes on by itself (null: it will not — the person said no, the setting is off, or no time is known). The
 * queue is paused `limited` until then: what the person sends waits for it.
 */
export interface LimitHold {
  provider: Provider;
  window: LimitWindow;
  at: Ms;
  resetsAt: Ms | null;
  continueAt: Ms | null;
}
/** One time the runtime went on by itself, and the message that went (its own `auto` one, or the person's queued one). */
export interface AutoContinue {
  at: Ms;
  why: AutoContinueWhy;
  clientId: ClientId;
  /** The message was the runtime's own word to go on (not the person's queued one). */
  own?: true;
}
/**
 * A rewind: the chat went on from before one of the person's messages with other words. The turns from that message
 * on were left behind after `afterTurnId` (null: before the first turn the feed read); GET .../rewinds/{n}/turns shows
 * them. rewind.undo takes the chat back until `undoUntil`.
 */
export interface RewindMark {
  at: Ms;
  afterTurnId: TurnId | null;
  turns: number;
  files: boolean;
  undoUntil: Ms;
}
/** GET .../rewind?entryId=: what a rewind to that message would do; `files` null when the folder has no snapshot. */
export interface RewindPlan {
  entryId: TurnId;
  turns: number;
  files: { at: Ms; changes: { path: string; change: FileChange }[]; more: number } | null;
}
/**
 * Where a conversation came from. `atTurn` is the turn of the source a fork branches after; `fromModel` is what the
 * source ran then. The source is not changed by either.
 */
export interface Origin {
  k: OriginKind;
  fromId: ConvId;
  fromTitle: string;
  fromProvider: Provider;
  fromModel: Model | null;
  atTurn?: TurnId;
  via: OriginVia;
  at: Ms;
}
/**
 * POST /conversations/{id}/handoff-draft: the text a handoff or a merge would carry, for the person to read and edit
 * before it goes. `tokens` is an estimate; `turns` is how many turns an excerpt kept (0 for a note).
 */
export interface HandoffArtifact {
  id: string;
  bytes: number;
}
interface HandoffDraftBase {
  tokens: number;
  turns: number;
}
export type HandoffDraft =
  | (HandoffDraftBase & { mode: 'history'; artifact: HandoffArtifact })
  | (HandoffDraftBase & { mode: Exclude<DraftMode, 'history'>; text: string });
export interface HandoffDraftRequest {
  mode: DraftMode;
  provider?: Provider;
  model?: string;
  /** The backend profile the context goes to: an excerpt is cut to that model's own window. */
  profile?: ProfileId;
}
/** When and why a chat's terminal was stopped; `idleMs` is how long it had stood idle (`by: idle` only). */
export interface TerminalStop {
  at: Ms;
  by: TerminalStopBy;
  idleMs?: number;
}
/** A provider-neutral terminal failure. The runtime does not resend or pick another target by itself. */
export interface TerminalFailure {
  kind: TerminalFailureKind;
  at: Ms;
  detail: string;
  terminal: true;
  needsTarget: true;
}
/** GET and PUT /api/v2/runtime-settings: the runtime's own settings, the machine's and not the person's. */
export interface RuntimeSettings {
  idleStop: IdleStop;
  /** A chat held on its usage limit goes on by itself at the reset; absent from an older runtime (it has no such). */
  limitContinue?: OnOff;
  /** A chat whose turn a restart cut short goes on by itself; absent from an older runtime. */
  restartContinue?: OnOff;
  /** What agents may do with the chats they delegate; absent: 5 chats each, only their own. */
  delegation?: DelegationSettings;
}
/** How many chats one chat's agent may have delegated at a time (1–10), and whether agents may write to any chat. */
export interface DelegationSettings {
  maxChildren: number;
  anyChat: boolean;
}
/** The delegation a runtime keeps when it was never set (wire.DefaultDelegation). */
export const DEFAULT_DELEGATION: DelegationSettings = { maxChildren: 5, anyChat: false };
/** The counts «Поручений на один чат» offers. */
export const DELEGATION_MAX_CHILDREN = [1, 2, 3, 5, 10] as const;

export type AgentState =
  | { k: 'starting'; since: Ms; step: LaunchStep; wait: Wait }
  | { k: 'working'; since: Ms; turnId: TurnId | null; compacting: boolean }
  | { k: 'waiting-question'; since: Ms; questionId: string }
  | { k: 'waiting-permission'; since: Ms; permissionId: string } // a plan waits here too, its kind is in Permission
  | { k: 'ready'; since: Ms } // the turn is over, the agent waits for input
  | { k: 'idle'; since: Ms } // the agent runs, no turn (new, interrupted)
  | { k: 'stopped'; since: Ms; resumable: boolean } // no agent process
  | { k: 'launch-failed'; since: Ms; reason: LaunchFailure }
  | { k: 'gone'; since: Ms; reason: GoneReason }
  | { k: 'unknown'; since: Ms }; // not checked yet; for delivery it is NOT idle

export interface Turn {
  id: TurnId; // the id of the user entry that opened the turn
  author: { provider: Provider; role: string | null };
  startedAt: Ms;
  endedAt: Ms | null;
  durationMs: number | null;
  toolCount: number;
  stepCount: number;
  status: TurnStatus;
  failure: TurnFailure | null;
  answerEntryId: EntryId | null; // the last reply, shown in the folded turn
  files: { path: string; op: FileOp }[];
  /** Absent when the turn made nothing to open. */
  outputs?: TurnOutput[];
}
export interface TurnFailure {
  code: TurnFailureCode;
  detail: string | null; // the agent's raw text: data, not a translation
  retryAt: Ms | null;
}
export type TurnSummary = Pick<
  Turn,
  'id' | 'status' | 'startedAt' | 'endedAt' | 'durationMs' | 'toolCount'
> & {
  snippet: string; // ≤ 160 characters
  failure?: TurnFailureCode; // why a failed turn failed (a limit is told from an error); absent otherwise
};
export interface TurnCounts {
  toolCount: number;
  stepCount: number;
}

export interface EntryBase {
  id: EntryId;
  turnId: TurnId | null;
  at: Ms;
  rev: number; // grows as the entry is appended to
}
export type Entry = EntryBase &
  (
    | { t: 'user'; text: string; attachments: AttachmentRef[]; clientId: ClientId | null }
    | { t: 'say'; md: string }
    | { t: 'think'; text: string }
    | {
        t: 'tool';
        toolUseId: string;
        tool: string;
        summary: string;
        ok: boolean | null;
        output: ToolOutput | null;
      }
    | {
        t: 'subagent';
        agentId: string;
        agentType: string;
        description: string;
        status: SubagentStatus;
        model: string | null;
        durationMs: number | null;
        output: ToolOutput | null; // what the agent returned, once it has
      }
    | { t: 'answers'; questionId: string; pairs: { q: string; a: string }[] }
    | { t: 'gate'; permissionId: string }
    | { t: 'compaction'; tokensBefore: number | null; tokensAfter: number | null; keep: string[] }
    | { t: 'notice'; level: NoticeLevel; code: string; detail: string | null }
    | { t: 'interrupt' }
    | { t: 'peer'; peers: PeerNote[] }
  );
/** A message another agent's session wrote to this one (Claude Code's teammate); `md` is its words, shown as text. */
export interface PeerNote {
  from: string;
  kind: PeerNoteKind;
  color: string | null;
  event: string | null; // the type of a notice the feed has no kind for
  summary: string | null;
  sentAt: Ms | null;
  agentId: string | null; // a background subagent of this session wrote it: `from` is its task
  model: string | null;
  md: string;
}
export interface ToolOutput {
  text: string;
  truncated: { shown: number; total: number } | null; // over 4 KB the rest is fetched on request
}
export interface AttachmentRef {
  id: string;
  name: string;
  kind: AttachmentKind;
  mime: string;
}
/**
 * A place a message points the agent at: a design (its id; target ''), a document (slug or path), a board, a file of
 * the project, a merge request, a task. The runtime appends their `<attached>` block to the message's text.
 */
export interface MessageRef {
  kind: RefKind;
  id: string;
  /** What the chip says. */
  title: string;
  /** Where it is for the agent: a URL or a path; '' for a design. */
  target: string;
}

export interface Message {
  clientId: ClientId; // UUIDv7 from the page; the idempotency key
  conversationId: ConvId;
  text: string;
  attachments: AttachmentRef[];
  /** The places it points at; `text` already ends with their block. Absent when none. */
  refs?: MessageRef[];
  mode: MessageMode;
  kind: MessageKind;
  /** Set on a message the runtime wrote itself to go on: drawn as a quiet line, not as the person's words. */
  auto?: AutoContinueWhy;
  /** The screen of the terminal that was closed before the message went in; absent when nothing was in the way. */
  cleared?: TerminalAside;
  createdAt: Ms;
  attempts: number;
  state: MessageState;
  rev: number;
}
/** A message's state on the wire. */
export type MessageState =
  | { k: 'queued'; position: number; reason: QueueReason; wait: Wait | null } // position 1 is the head; null: no term
  | { k: 'typing'; wait: Wait }
  | { k: 'in-input'; wait: Wait }
  | { k: 'accepted'; entryId: EntryId | null } // null: the hook confirmed it, the transcript line is not read yet
  | { k: 'in-turn'; turnId: TurnId }
  | { k: 'done'; turnId: TurnId }
  | { k: 'failed'; code: MessageFailure; retryable: boolean; at: Ms }
  | { k: 'cancelled'; by: 'user' };
/** A message's state on the page: the wire states and the ones before the runtime has it. */
export type LocalMessageState =
  { k: 'draft' } | { k: 'uploading'; done: number; total: number } | { k: 'submitting' } | MessageState;

export interface Queue {
  conversationId: ConvId;
  items: ClientId[]; // the head first
  paused: QueuePause | null;
  rev: number;
}
export interface QueuePause {
  reason: PauseReason;
  at: Ms;
}

// Question and permission ids share one space within a conversation, told apart by their prefix: 'q:<toolUseId>'
// and 'p:<toolUseId>' or 'p:<tabId>:<eventSeq>'. The runtime guarantees it.
export interface Question {
  id: string; // 'q:' and the tool use id of the AskUserQuestion
  conversationId: ConvId;
  turnId: TurnId;
  askedAt: Ms;
  items: QuestionItem[];
  state: GateState;
}
export interface QuestionItem {
  header: string;
  question: string;
  multi: boolean;
  options: { label: string; description: string | null; preview: string | null }[];
}
export interface Permission {
  id: string; // 'p:' and the tool use id, or 'p:<tabId>:<eventSeq>' when the agent gave none
  conversationId: ConvId;
  turnId: TurnId | null; // null outside a turn
  kind: PermissionKind;
  askedAt: Ms;
  tool: string | null;
  command: string | null;
  cwd: string | null;
  planMd: string | null;
  options: { id: string; label: string; scope: PermissionScope }[]; // read off the screen
  state: GateState;
}
export type GateState =
  | { k: 'open'; refused?: GateRefusal; autoDenyAt?: Ms } // refused: the menu on the screen is not the one the card shows, decide in the terminal; autoDenyAt: Claude denies the request by itself then
  | { k: 'answering'; opId: string; wait: Wait }
  | { k: 'answered'; at: Ms; byDevice: string } // «Вы ответили с другого устройства»
  | { k: 'closed'; reason: GateCloseReason };

/** What an agent whose prompt is free still waits for; the parts that are 0 are absent, they add up to bgAgents. */
export interface BgWait {
  agents?: number; // background subagents running
  teammates?: number; // named teammates at work (an idle one waits for a message and is not counted)
  commands?: number; // background shell commands running
  monitors?: number; // monitors watching
  workflows?: number; // dynamic workflows running
  command?: string; // the title of the command, when it is the only one
  wakeupAt?: Ms; // when the agent's own scheduled wakeup comes
}
export interface BackgroundTask {
  id: string;
  conversationId: ConvId;
  kind: BackgroundKind;
  title: string;
  startedAt: Ms;
  endedAt: Ms | null;
  status: BackgroundStatus;
  devServer: boolean;
  stoppable: boolean;
  outputPath: string | null;
  /** A command that is the agent's Monitor, not a shell; absent for the rest. */
  monitor?: true;
  /** A subagent that is a named teammate: the title is its name, the role what it was started for; only it is ever
   *  `idle` (done with what it was sent, waiting for the next message). Absent for the rest. */
  teammate?: true;
  /** A subagent's model (the alias or the full id), effort when known, kind and last action; absent when unknown. */
  model?: string;
  effort?: Effort;
  role?: string;
  last?: string;
  /** A workflow's run: its phases and the agents it started in each. Only a workflow has it. */
  workflow?: WorkflowRun;
}
/** A dynamic workflow: the name and description of its script, the Workflow call that started it (its tool entry in
 *  the feed) and its phases in the script's order, each with the agents started in it so far. */
export interface WorkflowRun {
  name: string;
  description: string;
  toolUseId: string | null;
  phases: WorkflowPhase[];
}
export interface WorkflowPhase {
  title: string;
  agents: WorkflowAgent[];
}
/** An agent a workflow started; its id names its transcript, so it opens in the pane as a subagent. Never `idle`. */
export interface WorkflowAgent {
  id: string;
  label: string;
  status: BackgroundStatus;
  startedAt: Ms | null;
  endedAt: Ms | null;
  model: string | null;
  last: string | null; // its last step, one line
  result: string | null; // the first words of what it returned
}
/** A picture the agent or a subagent looked at (a file it read, a screenshot a tool saved); its bytes are GET
 *  /api/v2/conversations/{id}/shot?id={id}. `agent` is the subagent that looked at it, absent for the agent itself. */
export interface Shot {
  id: string;
  name: string;
  at: Ms;
  agent?: string;
}
/** A picture a message of the conversation names by its path (the agent's «Скриншот: /tmp/…/home.png», a pasted
 *  upload's path): `text` is the path exactly as the message writes it. Listed only while the file may be shown; its
 *  bytes are GET /api/v2/conversations/{id}/shot?id={id}, as a shot's. */
export interface Picture {
  id: string;
  text: string;
}
export interface Now {
  turnId: TurnId;
  thought: string | null;
  action: { tool: string; summary: string; command: string | null } | null;
  word: string | null; // the live reply off the screen
  at: Ms;
}
export interface Limits {
  maxQueue: number;
  maxText: number; // UTF-16 units
  maxAttachments: number;
  maxImageBytes: number;
  maxFileBytes: number;
  slowMs: number; // a command unanswered this long shows «дольше обычного»
  cmdBudgetMs: number; // the runtime answers every command within this, refused{timeout} at worst
  cmdTimeoutMs: number; // the page gives up on a command after this and offers retry or cancel
}

/** The term of a state that waits on the runtime: past slowAt «дольше обычного»; by deadlineAt the runtime has moved
 * the state on (failed, launch-failed, back to open), so it never stays longer. */
export interface Wait {
  slowAt: Ms;
  deadlineAt: Ms;
}

/** Where a wait is at a moment of the machine's clock (the page's clock shifted by welcome.serverTime). */
export function waitPhase(wait: Wait, at: Ms): 'normal' | 'slow' | 'overdue' {
  if (at >= wait.deadlineAt) return 'overdue';
  return at >= wait.slowAt ? 'slow' : 'normal';
}

// ---- socket

export type ClientFrame =
  | { f: 'hello'; proto: 2; device: string; app: string } // device: the device's stable id
  | {
      f: 'sub';
      ch: Channel;
      epoch?: string;
      afterSeq?: number;
      afterEntryId?: EntryId;
      opts?: { load?: boolean };
    }
  | { f: 'unsub'; ch: Channel }
  | { f: 'ping'; t: number };

export interface MachineSnapshot {
  machine: Machine;
}
/**
 * Projects are the folders of the machine made projects, oldest first. defaultCwd is the folder a conversation starts
 * in when the person names none; empty when the machine offers none.
 */
export interface ConvsSnapshot {
  conversations: Conversation[];
  projects: Project[];
  defaultCwd: string;
  /** Absent while nothing is grouped or placed (and from a runtime that has no favorites). */
  favorites?: Favorites;
  /** The composers' drafts the machine keeps (empty ones too); absent while none, and from a runtime without them. */
  drafts?: Draft[];
}
/** Turns are the last turns, oldest first; entries are the steps of the last three and the answers of the rest. */
export interface ConvSnapshot {
  turns: Turn[];
  entries: Entry[];
  beforeTurnId: TurnId | null; // the cursor for older turns, null at the start
  queue: Queue;
  messages: Message[];
  questions: Question[];
  permissions: Permission[];
  background: BackgroundTask[];
  shots?: Shot[]; // newest first, at most 30; absent when none
  pictures?: Picture[]; // named in the messages, the latest last, at most 60; absent when none
  now: Now | null;
}
// The snapshot invariant: the hub subscribes the channel before it takes the snapshot, so nothing is lost between
// them. The page drops events with seq ≤ snapshot.seq, and applies every event idempotently by id and rev.
interface SnapshotBase {
  f: 'snapshot';
  seq: number; // the last seq the snapshot includes; 0 before any event
  mode: SnapshotMode; // after: what follows afterEntryId
}
export type SnapshotFrame =
  | (SnapshotBase & { ch: 'machine'; data: MachineSnapshot })
  | (SnapshotBase & { ch: 'convs'; data: ConvsSnapshot })
  | (SnapshotBase & { ch: ConvChannel; data: ConvSnapshot });

export type MachineEvent =
  | {
      type: 'machine_status';
      reach: Reach;
      runtime: RuntimeInfo | null;
      agents: MachineAgent[];
      /** Absent: the health the page holds stays as it was. */
      health?: MachineHealth;
    }
  | { type: 'machine_load'; load: MachineLoad };
export type ConvsEvent =
  | { type: 'conversation_upsert'; conv: Conversation }
  | { type: 'conversation_state'; id: ConvId; state: AgentState; rev: number }
  | { type: 'conversation_seen'; id: ConvId; seenAt: Ms; rev: number }
  | { type: 'conversation_removed'; id: ConvId }
  | { type: 'project_upsert'; project: Project }
  | { type: 'project_removed'; id: string } // after its chats' own upserts or removals
  | { type: 'favorites_upsert'; favorites: Favorites }
  | { type: 'draft_upsert'; draft: Draft };
export type ConvEvent =
  | { type: 'turn_started'; turn: Turn }
  | { type: 'step'; entry: Entry; counts: TurnCounts | null } // counts of the entry's turn after it
  | { type: 'entry_update'; entry: Entry }
  | { type: 'entry_replace'; oldId: EntryId; entry: Entry }
  | ({ type: 'now' } & Now)
  | { type: 'question_asked'; question: Question }
  | { type: 'permission_requested'; permission: Permission }
  | { type: 'plan_proposed'; permission: Permission }
  | { type: 'gate_updated'; id: string; state: GateState }
  | { type: 'compaction'; phase: CompactionPhase; entry: Entry }
  | { type: 'turn_finished'; turn: Turn }
  | { type: 'turn_failed'; turn: Turn }
  | { type: 'quota_exhausted'; failure: TerminalFailure }
  | { type: 'queue_changed'; queue: Queue }
  | { type: 'prompt_state'; clientId: ClientId; state: MessageState; rev: number }
  | { type: 'message_upsert'; message: Message }
  | { type: 'background_upsert'; task: BackgroundTask }
  | { type: 'background_removed'; id: string }
  | { type: 'shots_set'; shots: Shot[] }
  | { type: 'pictures_set'; pictures: Picture[] }
  // the feed as it is after a rewind or its undo: the page drops what it holds of the feed and takes this
  | { type: 'feed_reset'; turns: Turn[]; entries: Entry[]; beforeTurnId: TurnId | null };
export type Event = MachineEvent | ConvsEvent | ConvEvent;
export type EvFrame =
  | { f: 'ev'; ch: 'machine'; seq: number; e: MachineEvent }
  | { f: 'ev'; ch: 'convs'; seq: number; e: ConvsEvent }
  | { f: 'ev'; ch: ConvChannel; seq: number; e: ConvEvent };

export type ServerFrame =
  | {
      f: 'welcome';
      proto: 2;
      epoch: string; // new on every start of the runtime
      serverTime: Ms;
      machine: Machine;
      features: Feature[];
      limits: Limits;
    }
  | SnapshotFrame
  | EvFrame
  | { f: 'pong'; t: number }
  | { f: 'error'; ch?: Channel; code: FrameErrorCode };

/** The page's view of its socket; the runtime never sends it. */
export type Connection =
  | { k: 'connecting'; attempt: number }
  | { k: 'live'; since: Ms }
  | { k: 'stale'; since: Ms }
  | { k: 'offline'; retryAt: Ms; cause: 'network' | 'machine-unreachable' | 'unauthorized' | 'proto' };

// ---- commands: POST /api/v2/cmd/<op>

interface OpBody {
  opId: string; // the same opId with the same body gets the same result, with another body refused{conflict}
}
export interface CommandBodies {
  send: OpBody & {
    clientId: ClientId; // = opId
    conversationId: ConvId;
    text: string;
    attachmentIds: string[];
    mode: MessageMode;
    kind: MessageKind;
    /** The places it points at; the runtime appends their block to the text. */
    refs?: MessageRef[];
  };
  cancel: OpBody & { conversationId: ConvId; clientId: ClientId };
  retry: OpBody & { conversationId: ConvId; clientId: ClientId };
  promote: OpBody & { conversationId: ConvId; clientId: ClientId; to: PromoteTo };
  // answers, per item: the chosen options, or the person's own text instead of them (choices []) — never both
  answer: OpBody & { conversationId: ConvId; questionId: string } & (
      { answers: { i: number; choices: number[]; text?: string }[] } | { chatAbout: true }
    );
  decide: OpBody & { conversationId: ConvId; permissionId: string; optionId: string };
  interrupt: OpBody & { conversationId: ConvId; pauseQueue?: boolean }; // pauseQueue defaults to true
  'queue.resume': OpBody & { conversationId: ConvId };
  seen: OpBody & { conversationId: ConvId | 'all'; upTo: Ms };
  'conversation.create': OpBody & {
    provider: Provider;
    model: Model | null;
    first?: FirstMessage;
    /** A git worktree of its own, on a new branch off the folder's HEAD (a folder that is no repository is used as it is). */
    worktree?: boolean;
    launch?: Launch;
    /** Starts the chat on a backend profile of the provider's CLI, with one of its models; needs a saved key. */
    profile?: ProfileId;
  } & ({ projectId: string } | { cwd: string });
  'conversation.rename': OpBody & { id: ConvId; title: string };
  'conversation.retitle': OpBody & { id: ConvId };
  'conversation.pin': OpBody & { id: ConvId; pinned: boolean };
  'conversation.move': OpBody & { id: ConvId; projectId: string | null };
  'conversation.delete': OpBody & { id: ConvId; force?: boolean };
  'conversation.restore': OpBody & { id: ConvId };
  // A stopped conversation's own, or a session the agent CLI kept on the machine that no conversation holds (a new
  // conversation answers it).
  'conversation.resume': OpBody & ({ id: ConvId } | { provider: Provider; sessionId: string; cwd: string });
  // ultracode: turn the mode on or off with the change; absent keeps the mode the conversation runs.
  model: OpBody & { id: ConvId; model: string; effort: Effort | null; ultracode?: boolean };
  'bg.stop': OpBody & { conversationId: ConvId; taskId: string };
  'project.create': OpBody & { path?: string; name?: string };
  // path: the folder the project's next chats start in; name: what it is called. Absent keeps it; one is needed.
  'project.update': OpBody & { id: string; path?: string; name?: string };
  'project.delete': OpBody & { id: string; chats: ProjectChats };
  'favorites.group.create': OpBody & { name: string };
  'favorites.group.rename': OpBody & { id: string; name: string };
  'favorites.group.delete': OpBody & { id: string };
  'favorites.group.move': OpBody & { id: string; beforeId: string | null };
  'favorites.place': OpBody & { id: ConvId; groupId: string | null; beforeId: ConvId | null };
  // The /goal of a Claude session: typed into the agent as a queued command; empty text clears it. One line.
  goal: OpBody & { id: ConvId; text: string };
  'draft.set': OpBody & {
    key: ConvId | typeof DRAFT_NEW;
    text: string;
    attachments?: AttachmentRef[];
    refs?: MessageRef[];
    rev: number;
  };
  /** Puts a queued message before another queued one (null: at the end of the line). */
  'queue.move': OpBody & { conversationId: ConvId; clientId: ClientId; beforeClientId: ClientId | null };
  /** Ends the chat's terminal (the agent and all under its pane); the chat stays and the next message resumes it. */
  'terminal.stop': OpBody & { id: ConvId; force?: boolean };
  /** Goes on from before the person's message entryId with text in its place; files returns the folder too. */
  rewind: OpBody & { id: ConvId; entryId: TurnId; text: string; files: boolean };
  /** Takes the chat back to where its last rewind took it from, while that rewind's undoUntil has not passed. */
  'rewind.undo': OpBody & { id: ConvId };
  /** Branches the conversation off after a finished turn (the last one when absent) into a new conversation. */
  'conversation.fork': OpBody & { id: ConvId; atTurn?: TurnId };
  /** Opens a conversation on another agent or model whose first message is `text`, the approved context. */
  'conversation.handoff': OpBody & {
    id: ConvId;
    provider: Provider;
    model: Model | null;
    /** Starts the new chat on a backend profile; a handoff never inherits the profile it comes from. */
    profile?: ProfileId;
  } & ({ mode: 'history'; artifactId: string } | { mode: Exclude<HandoffMode, 'history'>; text: string });
  /** Sends a fork's result into the queue of the conversation it came from. */
  'conversation.merge': OpBody & { id: ConvId; text: string };
  /** Decides what a chat held on its usage limit does (refused not-found when it is not held). */
  limit: OpBody & { id: ConvId; action: LimitAction };
}
export interface FirstMessage {
  clientId: ClientId;
  text: string;
  attachmentIds: string[];
}
export type Command = { [K in Op]: { op: K; body: CommandBodies[K] } }[Op];

/** seq is the seq of ch from which the effect shows on the socket: the page waits for it, not for a timer. */
export type CmdResult =
  | { r: 'accepted' | 'queued'; ch: Channel; seq: number; rev?: number; id?: string }
  | { r: 'refused'; code: RefuseCode };

/** Every command ends in one of these on the page: the runtime's result, or no answer within limits.cmdTimeoutMs
 * (timeout), no connection (network) or an HTTP failure (http). The last three are retried with the same opId. */
export type CmdOutcome =
  | CmdResult
  | { r: 'failed'; code: 'timeout' | 'network'; status: null }
  | { r: 'failed'; code: 'http'; status: number };

// ---- HTTP pages

/** Carries the device id on every HTTP request, as hello does on the socket; made once, kept in localStorage. */
export const DEVICE_HEADER = 'X-Harnessly-Device';
/** The turns page's limit: 30 when not given; below 1 or above 100 the page answers refused{bad-request}. */
export const TURNS_PAGE_DEFAULT = 30;
export const TURNS_PAGE_MAX = 100;
/** What a page that cannot answer sends, as a command does. */
export type Refusal = Extract<CmdResult, { r: 'refused' }>;

export interface Hello {
  proto: number;
  version: string;
  features: Feature[];
}
export interface TurnsPage {
  turns: Turn[]; // oldest first
  entries: Entry[]; // their answer entries
  beforeTurnId: TurnId | null;
}
/**
 * The cluster's own copy of the person's chats, read from the gateway (not from a machine): the list with whether
 * each machine is on, and one chat as the turns page gives it, with the state of the machine it ran on.
 */
export const CENTRAL_STATES = ['online', 'offline'] as const;
export type CentralState = (typeof CENTRAL_STATES)[number];
export interface CentralList {
  /** false: the copy is off for this person (nothing is stored, nothing is served). */
  enabled: boolean;
  machines: { id: MachineId; name: string; state: CentralState }[];
  conversations: Conversation[];
  /** The chats continued on another machine: the source is held and the target goes on (absent from an older gateway). */
  moved?: CentralMove[];
}
export interface CentralMove {
  machineId: MachineId;
  convId: ConvId;
  toMachineId: MachineId;
  toConvId: ConvId;
}
/**
 * The person's sidebar the cluster keeps (not a machine's): the projects of the profile, each standing for
 * projects of the machines (`members`), and where the favorites stand, by chat id. Which chats are favorites stays each
 * chat's own `pinned`. `merged`: the machines whose own favorites' layout is already in.
 */
export interface ProfileMember {
  machineId: MachineId;
  projectId: string;
}
export interface ProfileProject {
  id: string;
  name: string;
  createdAt: Ms;
  members: ProfileMember[];
}
export interface ProfileSidebar {
  projects: ProfileProject[];
  favorites: Favorites;
  merged: MachineId[];
}
/** The gateway's sidebar: off when the cluster keeps none for the person; revision 0 and no document before the first write. */
export interface ProfileSidebarPage {
  enabled: boolean;
  rev: number;
  doc: ProfileSidebar | null;
}
/** The answer to a write: its revision, or `conflict` with the document that stands now. */
export interface ProfileSidebarWrite {
  rev: number;
  conflict?: boolean;
  doc?: ProfileSidebar | null;
}
export interface CentralChat extends TurnsPage {
  machine: CentralState;
  machineId: MachineId;
  conversation: Conversation;
}
export interface EntriesPage {
  turnId: TurnId;
  entries: Entry[];
}
export interface OutputPage {
  entryId: EntryId;
  output: ToolOutput;
}

// ---- strict parser
//
// Every frame from the runtime goes through parseServerFrame: an unknown kind, an unknown field, a missing field
// or a value outside a closed list is a ProtocolError, never a silent skip.
//
// In a production build the page may be older than the runtime (a tab left open over a deploy), so there it reads
// what it knows: a field it has no name for is dropped, and an optional field it cannot read is dropped. A field it
// needs but cannot read still fails the frame. Dev and tests stay strict so schema drift is caught before release.

export class ProtocolError extends Error {
  readonly path: string;
  constructor(path: string, problem: string) {
    super(`${path}: ${problem}`);
    this.name = 'ProtocolError';
    this.path = path;
  }
}

let lenient = import.meta.env.PROD;

/** Switches between the production reading (unknown things dropped) and the strict one; tests use it. */
export function setLenientDecoding(on: boolean): void {
  lenient = on;
}

type Decoder<T> = (v: unknown, path: string) => T;
type Shape = Record<string, Decoder<unknown>>;
type Out<S extends Shape> = { [K in keyof S]: S[K] extends Decoder<infer T> ? T : never };

const fail = (path: string, problem: string): never => {
  throw new ProtocolError(path, problem);
};

const str: Decoder<string> = (v, p) => (typeof v === 'string' ? v : fail(p, 'not a string'));
const id: Decoder<string> = (v, p) => (str(v, p) !== '' ? (v as string) : fail(p, 'empty'));
const bool: Decoder<boolean> = (v, p) => (typeof v === 'boolean' ? v : fail(p, 'not a boolean'));
const int: Decoder<number> = (v, p) =>
  typeof v === 'number' && Number.isSafeInteger(v) && v >= 0 ? v : fail(p, 'not a whole number ≥ 0');
const seq: Decoder<number> = (v, p) => (int(v, p) >= 1 ? (v as number) : fail(p, 'below 1'));
const num: Decoder<number> = (v, p) =>
  typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : fail(p, 'not a number ≥ 0');
const ms = int;

function constant<const V extends string | number | boolean>(c: V): Decoder<V> {
  return (v, p) => (v === c ? c : fail(p, `not ${JSON.stringify(c)}`));
}

function oneOf<const L extends readonly string[]>(values: L): Decoder<L[number]> {
  return (v, p) =>
    values.includes(v as string) ? (v as L[number]) : fail(p, `${JSON.stringify(v)} is unknown`);
}

function nullable<T>(d: Decoder<T>): Decoder<T | null> {
  return (v, p) => (v === null ? null : d(v, p));
}

function array<T>(d: Decoder<T>, min = 0): Decoder<T[]> {
  return (v, p) => {
    if (!Array.isArray(v)) return fail(p, 'not an array');
    if (v.length < min) return fail(p, `fewer than ${min} items`);
    return v.map((x, i) => d(x, `${p}[${i}]`));
  };
}

const record = (v: unknown, p: string): Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : fail(p, 'not an object');

function object<S extends Shape, O extends Shape = Record<never, never>>(
  req: S,
  opt?: O,
): Decoder<Out<S> & Partial<Out<O>>> {
  return (v, p) => {
    const o = record(v, p);
    const out: Record<string, unknown> = {};
    for (const k of Object.keys(o)) {
      const inReq = Object.hasOwn(req, k);
      const d = inReq ? req[k] : opt && Object.hasOwn(opt, k) ? opt[k] : undefined;
      if (!d) {
        if (lenient) continue;
        return fail(p, `unknown field ${JSON.stringify(k)}`);
      }
      if (lenient && !inReq) {
        try {
          out[k] = d(o[k], `${p}.${k}`);
        } catch (e) {
          if (!(e instanceof ProtocolError)) throw e;
        }
        continue;
      }
      out[k] = d(o[k], `${p}.${k}`);
    }
    for (const k of Object.keys(req)) if (!Object.hasOwn(o, k)) fail(p, `no ${JSON.stringify(k)}`);
    return out as Out<S> & Partial<Out<O>>;
  };
}

/** A union told apart by disc; every kind's shape lists its fields besides disc and the base. */
function variants<D extends string, M extends Record<string, Shape>, B extends Shape = Record<never, never>>(
  disc: D,
  kinds: M,
  base?: B,
  optional?: Partial<Record<keyof M, Shape>>,
): Decoder<{ [K in keyof M & string]: { [P in D]: K } & Out<B> & Out<M[K]> }[keyof M & string]> {
  const decoders = new Map(
    Object.entries(kinds).map(([k, shape]) => [
      k,
      object({ [disc]: constant(k), ...base, ...shape }, optional?.[k]),
    ]),
  );
  return (v, p) => {
    const kind = record(v, p)[disc];
    const d = typeof kind === 'string' ? decoders.get(kind) : undefined;
    if (!d) return fail(`${p}.${disc}`, `${JSON.stringify(kind)} is unknown`);
    return d(v, p) as never;
  };
}

/** A union told apart by which of its keys the object has. */
function byKey<A, B>(key: string, withKey: Decoder<A>, without: Decoder<B>): Decoder<A | B> {
  return (v, p) => (Object.hasOwn(record(v, p), key) ? withKey(v, p) : without(v, p));
}

const CONV_ID = /^tab-[A-Za-z0-9_-]+$/;
const convId: Decoder<ConvId> = (v, p) =>
  CONV_ID.test(str(v, p)) ? (v as ConvId) : fail(p, 'not a conversation id');
const convChannel: Decoder<ConvChannel> = (v, p) => {
  const s = str(v, p);
  return s.startsWith('conv:') && CONV_ID.test(s.slice(5))
    ? (s as ConvChannel)
    : fail(p, 'not a conv channel');
};
const ONE_ID = '(c:[0-9a-f-]+|x:[0-9]+)#[0-9]+';
const TURN_ID = new RegExp(`^${ONE_ID}$`);
const ENTRY_ID = new RegExp(`^(${ONE_ID}|live:${ONE_ID})$`);
const turnId: Decoder<TurnId> = (v, p) =>
  TURN_ID.test(str(v, p)) ? (v as string) : fail(p, 'not a turn id');
const entryId: Decoder<EntryId> = (v, p) =>
  ENTRY_ID.test(str(v, p)) ? (v as string) : fail(p, 'not an entry id');
const questionId: Decoder<string> = (v, p) =>
  /^q:./.test(str(v, p)) ? (v as string) : fail(p, 'not a question id');
const permissionId: Decoder<string> = (v, p) =>
  /^p:./.test(str(v, p)) ? (v as string) : fail(p, 'not a permission id');
const gateId: Decoder<string> = (v, p) =>
  /^[qp]:./.test(str(v, p)) ? (v as string) : fail(p, 'not a gate id');
const waitD: Decoder<Wait> = (v, p) => {
  const w = object({ slowAt: ms, deadlineAt: ms })(v, p);
  return w.deadlineAt >= w.slowAt ? w : fail(`${p}.deadlineAt`, 'before slowAt');
};
const channel: Decoder<Channel> = (v, p) => (v === 'machine' || v === 'convs' ? v : convChannel(v, p));

const effort = oneOf(EFFORTS);
const provider = oneOf(PROVIDERS);
const feature = oneOf(FEATURES);

const runtimeInfo: Decoder<RuntimeInfo> = object({ version: str, proto: int, features: array(feature) });
const agentModel: Decoder<AgentModel> = object(
  {
    id,
    label: id,
    hint: str,
    efforts: array(effort),
    defaultEffort: nullable(effort),
  },
  { ultracode: bool },
);
const PROFILE_ID = /^[a-z][a-z0-9-]{0,31}$/;
const profileId: Decoder<ProfileId> = (v, p) =>
  PROFILE_ID.test(str(v, p)) ? (v as string) : fail(p, 'not a profile id');
const profileVendor = oneOf(PROFILE_VENDORS);
const profileOffer: Decoder<ProfileOffer> = object({
  id: profileId,
  label: id,
  vendor: profileVendor,
  configured: bool,
  models: array(agentModel),
});
const machineAgent: Decoder<MachineAgent> = object(
  {
    provider,
    version: nullable(str),
    status: oneOf(AGENT_INSTALL_STATUSES),
    models: array(agentModel),
    canSwitchModel: bool,
  },
  { profiles: array(profileOffer) },
);
const PROFILE_TAIL = /^….{4}$/u;
const profileKey: Decoder<ProfileKey> = object({
  configured: bool,
  tail: nullable((v, p) => (PROFILE_TAIL.test(str(v, p)) ? (v as string) : fail(p, 'not a key tail'))),
});
const profileList: Decoder<ProfileList> = object({
  profiles: array(
    object(
      {
        id: profileId,
        label: id,
        vendor: profileVendor,
        via: provider,
        host: id,
        hint: str,
        key: profileKey,
        models: array(agentModel),
      },
      { login: object({ account: str }) },
    ),
  ),
});
const profileTest: Decoder<ProfileTest> = object({
  ok: bool,
  reason: nullable(oneOf(PROFILE_TEST_REASONS)),
  status: nullable(int),
  ms: int,
});
const loginFlow: Decoder<LoginFlow> = object({
  id: str,
  provider,
  mode: oneOf(LOGIN_MODES),
  state: oneOf(LOGIN_STATES),
  input: oneOf(LOGIN_INPUTS),
  url: nullable(str),
  userCode: nullable(str),
  startedAt: ms,
  expiresAt: ms,
  reason: nullable(oneOf(LOGIN_REASONS)),
});
const loginStatus: Decoder<LoginStatus> = object({ flow: nullable(loginFlow) });
const loginOK: Decoder<LoginOK> = object({ ok: bool });
const machineLoad: Decoder<MachineLoad> = object({
  at: ms,
  cpu: num,
  memUsed: int,
  memTotal: int,
  diskFree: int,
  diskTotal: int,
  busyAgents: int,
});
const machineHealth: Decoder<MachineHealth> = object({
  level: oneOf(HEALTH_LEVELS),
  since: ms,
  issues: array(object({ metric: oneOf(HEALTH_METRICS), level: oneOf(['warn', 'critical'] as const) })),
});
const machine: Decoder<Machine> = object(
  {
    id,
    name: str,
    host: str,
    reach: oneOf(REACHES),
    lastSeenAt: nullable(ms),
    runtime: nullable(runtimeInfo),
    agents: array(machineAgent),
  },
  { load: machineLoad, health: machineHealth },
);
/** Ultracode was a level of the effort before it became a mode: an effort "ultracode" is xhigh with the mode on. */
const LEGACY_ULTRACODE = 'ultracode';
function legacyUltracode<T>(d: Decoder<T>): Decoder<T> {
  return (v, p) => {
    const o = record(v, p);
    return o.effort === LEGACY_ULTRACODE ? d({ ...o, effort: 'xhigh', ultracode: true }, p) : d(v, p);
  };
}
const model: Decoder<Model> = legacyUltracode(
  object({ id, effort: nullable(effort) }, { ultracode: bool, profile: profileId }),
);

const agentStateKinds = {
  starting: { since: ms, step: oneOf(LAUNCH_STEPS), wait: waitD },
  working: { since: ms, turnId: nullable(turnId), compacting: bool },
  'waiting-question': { since: ms, questionId },
  'waiting-permission': { since: ms, permissionId },
  ready: { since: ms },
  idle: { since: ms },
  stopped: { since: ms, resumable: bool },
  'launch-failed': { since: ms, reason: oneOf(LAUNCH_FAILURES) },
  gone: { since: ms, reason: oneOf(GONE_REASONS) },
  unknown: { since: ms },
} satisfies Record<AgentStateKind, Shape>;
const agentState: Decoder<AgentState> = variants('k', agentStateKinds);

const turnStatus = oneOf(TURN_STATUSES);
const turnFailure: Decoder<TurnFailure> = object({
  code: oneOf(TURN_FAILURE_CODES),
  detail: nullable(str),
  retryAt: nullable(ms),
});
const turn: Decoder<Turn> = (v, p) => {
  const t = object(
    {
      id: turnId,
      author: object({ provider, role: nullable(str) }),
      startedAt: ms,
      endedAt: nullable(ms),
      durationMs: nullable(int),
      toolCount: int,
      stepCount: int,
      status: turnStatus,
      failure: nullable(turnFailure),
      answerEntryId: nullable(entryId),
      files: array(object({ path: str, op: oneOf(FILE_OPS) })),
    },
    {
      outputs: array(
        object(
          { kind: oneOf(OUTPUT_KINDS), op: oneOf(OUTPUT_OPS), title: str, ref: str },
          { name: str, mime: str, size: int, sha256: str },
        ),
      ),
    },
  )(v, p);
  if ((t.status === 'failed') !== (t.failure !== null))
    fail(`${p}.failure`, `does not fit a ${t.status} turn`);
  if (t.status === 'live' && t.endedAt !== null) fail(`${p}.endedAt`, 'on a live turn');
  return t;
};
const snippet: Decoder<string> = (v, p) =>
  [...str(v, p)].length <= 160 ? (v as string) : fail(p, 'over 160');
const turnSummary: Decoder<TurnSummary> = object(
  {
    id: turnId,
    status: turnStatus,
    startedAt: ms,
    endedAt: nullable(ms),
    durationMs: nullable(int),
    toolCount: int,
    snippet,
  },
  { failure: oneOf(TURN_FAILURE_CODES) },
);
const turnCounts: Decoder<TurnCounts> = object({ toolCount: int, stepCount: int });
const queuePause: Decoder<QueuePause> = object({ reason: oneOf(PAUSE_REASONS), at: ms });

const project: Decoder<Project> = object({
  id,
  machineId: id,
  path: str,
  name: str,
  git: bool,
  createdAt: ms,
});

const FAV_GROUP_ID = /^fg-[A-Za-z0-9]{4,40}$/;
const favGroupId: Decoder<string> = (v, p) =>
  FAV_GROUP_ID.test(str(v, p)) ? (v as string) : fail(p, 'not a favorites group id');
const favName: Decoder<string> = (v, p) => {
  const name = str(v, p);
  return name.trim() !== '' && [...name].length <= 60 && !/[\r\n]/.test(name)
    ? name
    : fail(p, 'one line of at most 60 characters');
};

const favGroup: Decoder<FavGroup> = object({ id: favGroupId, name: favName, chats: array(convId) });
const favorites: Decoder<Favorites> = object({ groups: array(favGroup), loose: array(convId) });

const SESSION_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const sessionId: Decoder<string> = (v, p) =>
  SESSION_ID.test(str(v, p)) ? (v as string) : fail(p, 'not a session UUID');
const absolutePath: Decoder<string> = (v, p) =>
  str(v, p).startsWith('/') ? (v as string) : fail(p, 'not an absolute path');

const agentSession: Decoder<AgentSession> = object({
  provider,
  sessionId,
  title: id,
  cwd: absolutePath,
  updatedAt: ms,
  turns: int,
});

const terminal: Decoder<Terminal> = object({
  id: (v, p) => (/^[a-z0-9]{6,32}$/.test(str(v, p)) ? (v as string) : fail(p, 'not a terminal id')),
  createdAt: ms,
});

const designKind = oneOf(DESIGN_KINDS);
const designRole = oneOf(DESIGN_ROLES);
const designBoard: Decoder<DesignBoard> = object({ title: id, path: id });
const designOwner: Decoder<DesignOwner> = object({ name: id, me: bool });
const designArtifact: Decoder<DesignArtifact> = object(
  {
    id,
    title: id,
    updatedAt: ms,
    kind: designKind,
    pinned: bool,
    cover: nullable(id),
    projectPath: str,
    boards: array(designBoard),
    convs: array(convId),
  },
  { touchedBy: array(convId), owner: designOwner, role: designRole },
);
const designSystemInfo: Decoder<DesignSystemInfo> = object({ id, label: id, desc: str });
const designlyPin: Decoder<DesignlyPin> = object({ id, pinned: bool });

const taskStatus = oneOf(TASK_STATUSES);
const task: Decoder<Task> = object({ id, subject: str, status: taskStatus });
const convContext: Decoder<ConvContext> = object({ usedTokens: int, limitTokens: int });
const messageHit: Decoder<MessageHit> = object({
  convId,
  entryId,
  turnId: nullable(turnId),
  at: ms,
  role: oneOf(MESSAGE_ROLES),
  snippet: str,
});
const messageSearch: Decoder<MessageSearch> = object({ query: str, hits: array(messageHit), indexing: bool });
const chatDigest: Decoder<ChatDigest> = object({ convId, title: str, text: str, path: id, truncated: bool });
const share: Decoder<number> = (v, p) => (num(v, p) <= 1 ? (v as number) : fail(p, 'above 1'));
const diskPartId = oneOf(DISK_PART_IDS);
const machineDiskPart: Decoder<MachineDiskPart> = object({ id: diskPartId, bytes: int });
const machineDisk: Decoder<MachineDisk> = object({
  path: id,
  total: int,
  free: int,
  parts: array(machineDiskPart),
  measuredAt: nullable(ms),
  partial: bool,
});
const machineSample: Decoder<MachineSample> = object({ at: ms, cpu: share, mem: share, rx: num, tx: num });
const START_TIME = /^(0|[1-9][0-9]*)$/;
const startTime: Decoder<string> = (v, p) =>
  START_TIME.test(str(v, p)) ? (v as string) : fail(p, 'not a start time');
const port: Decoder<number> = (v, p) =>
  int(v, p) >= 1 && (v as number) <= 65535 ? (v as number) : fail(p, 'not a port');
const machineProc: Decoder<MachineProc> = object({
  pid: (v, p) => (int(v, p) >= 2 ? (v as number) : fail(p, 'below 2')),
  startTime,
  name: id,
  command: str,
  cpu: num,
  rss: int,
  kind: oneOf(MACHINE_PROC_KINDS),
  port: nullable(port),
  stuck: bool,
  guard: bool,
});
const machineStats: Decoder<MachineStats> = object({
  at: ms,
  cpu: share,
  cores: array(share),
  mem: object({ used: int, total: int }),
  net: object({ rx: num, tx: num }),
  disk: machineDisk,
  history: array(machineSample),
  procs: array(machineProc),
});
const machineStopped: Decoder<MachineStopped> = object({
  stopped: constant(true),
  signal: oneOf(['SIGTERM', 'SIGKILL'] as const),
});
const WEB_LINK = /^https?:\/\/[^/]+/;
const webLink: Decoder<string> = (v, p) => (WEB_LINK.test(str(v, p)) ? (v as string) : fail(p, 'not a link'));
const appRef: Decoder<AppRef> = object({ id, title: id, url: webLink });
const appChild: Decoder<AppChild> = object({
  id,
  title: id,
  url: webLink,
  state: nullable(id),
  done: bool,
  person: nullable(id),
});
const appMr: Decoder<AppMR> = object({
  source: nullable(id),
  draft: bool,
  conflicts: bool,
  needsRebase: bool,
  ready: bool,
  unresolved: bool,
  comments: int,
  pipeline: nullable(oneOf(APP_PIPELINES)),
  approvals: nullable(object({ given: int, required: int })),
});
const appRow: Decoder<AppRow> = object({
  key: id,
  id,
  title: id,
  url: webLink,
  state: nullable(id),
  flag: nullable(oneOf(APP_FLAGS)),
  tone: nullable(oneOf(APP_TONES)),
  project: nullable(id),
  person: nullable(id),
  target: nullable(id),
  createdAt: ms,
  updatedAt: ms,
  closedAt: nullable(ms),
  author: nullable(id),
  assignees: array(id),
  reviewers: array(id),
  me: array(oneOf(APP_ROLES)),
  labels: array(id),
  priority: nullable(id),
  kind: nullable(id),
  parent: nullable(appRef),
  progress: nullable(object({ done: int, total: int })),
  children: array(appChild),
  mr: nullable(appMr),
});
const appFeed: Decoder<AppFeed> = (v, p) => {
  const feed = object({
    id: oneOf(APP_IDS),
    state: oneOf(APP_STATES),
    reason: nullable(oneOf(APP_REASONS)),
    host: nullable(id),
    me: nullable(id),
    sections: array(object({ id: oneOf(APP_SECTION_IDS), rows: array(appRow) })),
  })(v, p);
  if ((feed.state === 'ready') !== (feed.reason === null)) fail(p, 'reason does not go with the state');
  if (feed.state !== 'ready' && feed.sections.length > 0) fail(p, 'an app that is not ready has no sections');
  return feed;
};
const appsFeed: Decoder<AppsFeed> = object({
  at: ms,
  staleDays: (v, p) => (int(v, p) >= 1 ? (v as number) : fail(p, 'below 1')),
  apps: array(appFeed),
});
const openChoice: Decoder<OpenChoice> = object({
  tab: oneOf(OPEN_TABS),
  target: id,
  label: id,
  detail: nullable(id),
});
const openTarget: Decoder<OpenTarget> = object(
  { tab: oneOf(OPEN_TABS), target: id },
  {
    choices: (v, p) => {
      const choices = array(openChoice)(v, p);
      return choices.length >= 2 ? choices : fail(p, 'fewer than 2 choices');
    },
  },
);
const openedRefs: Decoder<OpenedRefs> = object({
  refs: array(object({ ref: id, open: nullable(openTarget) })),
});
const optionValue: Decoder<AgentOptionValue> = (v, p) =>
  v === null || typeof v === 'string' || typeof v === 'boolean' ? v : int(v, p);
const ENV_NAME = /^[A-Z_][A-Z0-9_]{0,63}$/;
const envName: Decoder<string> = (v, p) =>
  ENV_NAME.test(str(v, p)) ? (v as string) : fail(p, 'not a variable name');
const MCP_NAME = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;
const agentSettings: Decoder<AgentSettings> = object({
  provider: oneOf(PROVIDERS),
  path: id,
  rev: id,
  account: object({
    signedIn: bool,
    method: nullable(oneOf(AGENT_ACCOUNT_METHODS)),
    plan: nullable(id),
    who: nullable(id),
  }),
  options: array(object({ key: oneOf(AGENT_OPTION_KEYS), value: optionValue })),
  rules: nullable(object({ allow: array(id), ask: array(id), deny: array(id) })),
  env: array(object({ key: envName, value: str, secret: bool })),
  mcp: array(
    object({
      name: (v, p) => (MCP_NAME.test(str(v, p)) ? (v as string) : fail(p, 'not a server name')),
      kind: oneOf(AGENT_MCP_KINDS),
      target: id,
      enabled: bool,
    }),
  ),
});
const legacyTab: Decoder<LegacyTab> = object({
  tabId: id,
  workspaceId: str,
  sessionName: id,
  jsonlPath: nullable(id),
  agentSessionId: nullable(id),
  cwd: str,
});
const fileKind = oneOf(FILE_KINDS);
const fileEntry: Decoder<FileEntry> = object({ name: id, path: id, kind: fileKind });

const dir: Decoder<Dir> = object({ path: id, name: id, git: bool, branch: nullable(id), empty: bool });

const launch: Decoder<Launch> = object({ id, prompt: str });
const goal: Decoder<Goal> = object({ text: str, status: oneOf(GOAL_STATUSES), since: ms });
const recap: Decoder<Recap> = object({ text: str, at: ms });
const pendingModel: Decoder<PendingModel> = legacyUltracode(
  object(
    {
      id,
      effort: nullable(effort),
      since: ms,
      applying: bool,
    },
    { ultracode: bool },
  ),
);
const modelSwitch: Decoder<ModelSwitch> = object(
  { at: ms, from: nullable(model), to: model },
  { refused: oneOf(REFUSE_CODES) },
);
const bgWait: Decoder<BgWait> = object(
  {},
  { agents: int, teammates: int, commands: int, monitors: int, workflows: int, command: str, wakeupAt: ms },
);
const terminalStop: Decoder<TerminalStop> = object({ at: ms, by: oneOf(TERMINAL_STOP_BYS) }, { idleMs: int });
const terminalFailure: Decoder<TerminalFailure> = object({
  kind: oneOf(TERMINAL_FAILURE_KINDS),
  at: ms,
  detail: str,
  terminal: constant(true),
  needsTarget: constant(true),
});
const runtimeSettings: Decoder<RuntimeSettings> = object(
  { idleStop: oneOf(IDLE_STOPS) },
  {
    limitContinue: oneOf(ON_OFFS),
    restartContinue: oneOf(ON_OFFS),
    delegation: object({ maxChildren: int, anyChat: bool }),
  },
);
const rewindMark: Decoder<RewindMark> = object({
  at: ms,
  afterTurnId: nullable(turnId),
  turns: int,
  files: bool,
  undoUntil: ms,
});
const rewindPlan: Decoder<RewindPlan> = object({
  entryId: turnId,
  turns: int,
  files: nullable(
    object({ at: ms, changes: array(object({ path: id, change: oneOf(FILE_CHANGES) })), more: int }),
  ),
});
const origin: Decoder<Origin> = object(
  {
    k: oneOf(ORIGIN_KINDS),
    fromId: convId,
    fromTitle: str,
    fromProvider: provider,
    fromModel: nullable(model),
    via: oneOf(ORIGIN_VIAS),
    at: ms,
  },
  { atTurn: turnId },
);
const handoffText: Decoder<string> = (v, p) =>
  [...id(v, p)].length <= HANDOFF_TEXT_MAX ? (v as string) : fail(p, 'too long');
const artifactId: Decoder<string> = (v, p) =>
  /^[a-f0-9]{32}$/.test(str(v, p)) ? (v as string) : fail(p, 'not an artifact id');
const handoffArtifact: Decoder<HandoffArtifact> = object({ id: artifactId, bytes: seq });
const handoffDraft: Decoder<HandoffDraft> = variants(
  'mode',
  {
    excerpt: { text: id },
    note: { text: id },
    result: { text: id },
    history: { artifact: handoffArtifact },
  },
  { tokens: int, turns: int },
);
const limitHold: Decoder<LimitHold> = object({
  provider: oneOf(PROVIDERS),
  window: oneOf(LIMIT_WINDOWS),
  at: ms,
  resetsAt: nullable(ms),
  continueAt: nullable(ms),
});
const autoContinue: Decoder<AutoContinue> = object(
  { at: ms, why: oneOf(AUTO_CONTINUE_WHYS), clientId: id },
  { own: constant(true) },
);
const conversation: Decoder<Conversation> = object(
  {
    id: convId,
    machineId: id,
    projectId: nullable(id),
    cwd: str,
    provider,
    model: nullable(model),
    title: str,
    titleSource: oneOf(TITLE_SOURCES),
    pinned: bool,
    createdAt: ms,
    lastActivityAt: ms,
    lastPromptAt: ms,
    state: agentState,
    seenAt: ms,
    lastTurn: nullable(turnSummary),
    queue: object({ count: int, paused: nullable(queuePause) }),
    gate: nullable(object({ kind: oneOf(GATE_KINDS), id: gateId })),
    rev: seq,
  },
  {
    goal,
    launch,
    recap,
    pendingModel,
    modelLog: array(modelSwitch),
    bgAgents: int,
    bgWait,
    terminalStop,
    terminalFailure,
    rewinds: array(rewindMark),
    origin,
    limited: limitHold,
    autoContinues: array(autoContinue),
    parentId: convId,
  },
);

const REF_TITLE_MAX = 200;
const REF_TARGET_MAX = 4096;
const bounded =
  (max: number, d: Decoder<string>): Decoder<string> =>
  (v, p) => {
    const s = d(v, p);
    return [...s].length <= max ? s : fail(p, `longer than ${max} characters`);
  };
const messageRef: Decoder<MessageRef> = object({
  kind: oneOf(REF_KINDS),
  id: bounded(REF_TARGET_MAX, id),
  title: bounded(REF_TITLE_MAX, str),
  target: bounded(REF_TARGET_MAX, str),
});
const messageRefs: Decoder<MessageRef[]> = (v, p) => {
  const refs = array(messageRef)(v, p);
  return refs.length <= REFS_MAX ? refs : fail(p, `more than ${REFS_MAX}`);
};
const draftKey: Decoder<Draft['key']> = (v, p) => (v === DRAFT_NEW ? DRAFT_NEW : convId(v, p));
const draftText: Decoder<string> = (v, p) =>
  [...str(v, p)].length <= DRAFT_TEXT_MAX
    ? (v as string)
    : fail(p, `longer than ${DRAFT_TEXT_MAX} characters`);
/** As many files as a message takes. */
export const DRAFT_ATTACHMENTS_MAX = 20;
const draftFiles: Decoder<AttachmentRef[]> = (v, p) => {
  const files = array(attachmentRef)(v, p);
  return files.length <= DRAFT_ATTACHMENTS_MAX ? files : fail(p, `more than ${DRAFT_ATTACHMENTS_MAX}`);
};
const draft: Decoder<Draft> = object(
  { key: draftKey, text: draftText, rev: seq, deviceId: id, at: ms },
  { attachments: draftFiles, refs: messageRefs },
);

const attachmentRef: Decoder<AttachmentRef> = object({
  id,
  name: str,
  kind: oneOf(ATTACHMENT_KINDS),
  mime: str,
});
const toolOutput: Decoder<ToolOutput> = object({
  text: str,
  truncated: nullable(object({ shown: int, total: int })),
});
const entryKinds = {
  user: { text: str, attachments: array(attachmentRef), clientId: nullable(id) },
  say: { md: str },
  think: { text: str },
  tool: { toolUseId: id, tool: str, summary: str, ok: nullable(bool), output: nullable(toolOutput) },
  subagent: {
    agentId: id,
    agentType: str,
    description: str,
    status: oneOf(SUBAGENT_STATUSES),
    model: nullable(str),
    durationMs: nullable(int),
    output: nullable(toolOutput),
  },
  answers: { questionId, pairs: array(object({ q: str, a: str })) },
  gate: { permissionId },
  compaction: { tokensBefore: nullable(int), tokensAfter: nullable(int), keep: array(str) },
  notice: { level: oneOf(NOTICE_LEVELS), code: id, detail: nullable(str) },
  interrupt: {},
  peer: {
    peers: array(
      object({
        from: str,
        kind: oneOf(PEER_NOTE_KINDS),
        color: nullable(str),
        event: nullable(str),
        summary: nullable(str),
        sentAt: nullable(ms),
        agentId: nullable(str),
        model: nullable(str),
        md: str,
      }),
    ),
  },
} satisfies Record<EntryKind, Shape>;
const entry: Decoder<Entry> = variants('t', entryKinds, {
  id: entryId,
  turnId: nullable(turnId),
  at: ms,
  rev: seq,
});

const messageStateKinds = {
  queued: { position: seq, reason: oneOf(QUEUE_REASONS), wait: nullable(waitD) },
  typing: { wait: waitD },
  'in-input': { wait: waitD },
  accepted: { entryId: nullable(entryId) },
  'in-turn': { turnId },
  done: { turnId },
  failed: { code: oneOf(MESSAGE_FAILURES), retryable: bool, at: ms },
  cancelled: { by: constant('user') },
} satisfies Record<MessageStateKind, Shape>;
const messageState: Decoder<MessageState> = variants('k', messageStateKinds);
const message: Decoder<Message> = object(
  {
    clientId: id,
    conversationId: convId,
    text: str,
    attachments: array(attachmentRef),
    mode: oneOf(MESSAGE_MODES),
    kind: oneOf(MESSAGE_KINDS),
    createdAt: ms,
    attempts: int,
    state: messageState,
    rev: seq,
  },
  { refs: messageRefs, auto: oneOf(AUTO_CONTINUE_WHYS), cleared: oneOf(TERMINAL_ASIDES) },
);
const messageHanded: Decoder<MessageHanded> = object({
  at: ms,
  where: oneOf(HANDED_WHERES),
  entryId: nullable(entryId),
});
const messageStatusKinds = {
  known: { message },
  unknown: {},
  expired: { at: ms },
} satisfies Record<MessageStatusKind, Shape>;
const messageStatus: Decoder<MessageStatus> = variants(
  'k',
  messageStatusKinds,
  { clientId: id },
  { known: { handed: messageHanded } },
);
const queue: Decoder<Queue> = object({
  conversationId: convId,
  items: array(id),
  paused: nullable(queuePause),
  rev: seq,
});

const gateStateKinds = {
  open: {},
  answering: { opId: id, wait: waitD },
  answered: { at: ms, byDevice: id },
  closed: { reason: oneOf(GATE_CLOSE_REASONS) },
} satisfies Record<GateStateKind, Shape>;
const gateState: Decoder<GateState> = variants('k', gateStateKinds, undefined, {
  open: { refused: oneOf(GATE_REFUSALS), autoDenyAt: ms },
});
const question: Decoder<Question> = object({
  id: questionId,
  conversationId: convId,
  turnId,
  askedAt: ms,
  items: array(
    object({
      header: str,
      question: str,
      multi: bool,
      options: array(object({ label: str, description: nullable(str), preview: nullable(str) })),
    }),
  ),
  state: gateState,
});
const permissionOf = (kind?: PermissionKind): Decoder<Permission> => {
  const d: Decoder<Permission> = object({
    id: permissionId,
    conversationId: convId,
    turnId: nullable(turnId),
    kind: oneOf(PERMISSION_KINDS),
    askedAt: ms,
    tool: nullable(str),
    command: nullable(str),
    cwd: nullable(str),
    planMd: nullable(str),
    options: array(object({ id, label: str, scope: oneOf(PERMISSION_SCOPES) })),
    state: gateState,
  });
  return (v, p) => {
    const perm = d(v, p);
    if (kind && perm.kind !== kind) fail(`${p}.kind`, `not ${kind}`);
    return perm;
  };
};
const workflowAgent: Decoder<WorkflowAgent> = object({
  id,
  label: str,
  status: oneOf(BACKGROUND_STATUSES),
  startedAt: nullable(ms),
  endedAt: nullable(ms),
  model: nullable(str),
  last: nullable(str),
  result: nullable(str),
});
const workflowRun: Decoder<WorkflowRun> = object({
  name: str,
  description: str,
  toolUseId: nullable(id),
  phases: array(object({ title: str, agents: array(workflowAgent) })),
});
const backgroundTask: Decoder<BackgroundTask> = object(
  {
    id,
    conversationId: convId,
    kind: oneOf(BACKGROUND_KINDS),
    title: str,
    startedAt: ms,
    endedAt: nullable(ms),
    status: oneOf(BACKGROUND_STATUSES),
    devServer: bool,
    stoppable: bool,
    outputPath: nullable(str),
  },
  {
    monitor: constant(true),
    teammate: constant(true),
    model: str,
    effort,
    role: str,
    last: str,
    workflow: workflowRun,
  },
);
const shot: Decoder<Shot> = object({ id, name: str, at: ms }, { agent: str });
const picture: Decoder<Picture> = object({ id, text: str });
const nowShape = {
  turnId,
  thought: nullable(str),
  action: nullable(object({ tool: str, summary: str, command: nullable(str) })),
  word: nullable(str),
  at: ms,
};
const now: Decoder<Now> = object(nowShape);
const limits: Decoder<Limits> = object({
  maxQueue: int,
  maxText: int,
  maxAttachments: int,
  maxImageBytes: int,
  maxFileBytes: int,
  slowMs: int,
  cmdBudgetMs: int,
  cmdTimeoutMs: int,
});

const machineSnapshot: Decoder<MachineSnapshot> = object({ machine });
const convsSnapshot: Decoder<ConvsSnapshot> = object(
  {
    conversations: array(conversation),
    projects: array(project),
    defaultCwd: str,
  },
  { favorites, drafts: array(draft) },
);
const convSnapshot: Decoder<ConvSnapshot> = object(
  {
    turns: array(turn),
    entries: array(entry),
    beforeTurnId: nullable(turnId),
    queue,
    messages: array(message),
    questions: array(question),
    permissions: array(permissionOf()),
    background: array(backgroundTask),
    now: nullable(now),
  },
  { shots: array(shot), pictures: array(picture) },
);

const finishedTurn: Decoder<Turn> = (v, p) => {
  const t = turn(v, p);
  return t.status === 'done' || t.status === 'interrupted'
    ? t
    : fail(`${p}.status`, `${t.status} in turn_finished`);
};
const failedTurn: Decoder<Turn> = (v, p) => {
  const t = turn(v, p);
  return t.status === 'failed' ? t : fail(`${p}.status`, `${t.status} in turn_failed`);
};
const compactionEntry: Decoder<Entry> = (v, p) => {
  const e = entry(v, p);
  return e.t === 'compaction' ? e : fail(`${p}.t`, 'not compaction');
};

const machineEventKinds = {
  machine_status: { reach: oneOf(REACHES), runtime: nullable(runtimeInfo), agents: array(machineAgent) },
  machine_load: { load: machineLoad },
} satisfies Record<MachineEventType, Shape>;
const convsEventKinds = {
  conversation_upsert: { conv: conversation },
  conversation_state: { id: convId, state: agentState, rev: seq },
  conversation_seen: { id: convId, seenAt: ms, rev: seq },
  conversation_removed: { id: convId },
  project_upsert: { project },
  project_removed: { id },
  favorites_upsert: { favorites },
  draft_upsert: { draft },
} satisfies Record<ConvsEventType, Shape>;
const convEventKinds = {
  turn_started: { turn },
  step: { entry, counts: nullable(turnCounts) },
  entry_update: { entry },
  entry_replace: { oldId: entryId, entry },
  now: nowShape,
  question_asked: { question },
  permission_requested: { permission: permissionOf('tool') },
  plan_proposed: { permission: permissionOf('plan') },
  gate_updated: { id: gateId, state: gateState },
  compaction: { phase: oneOf(COMPACTION_PHASES), entry: compactionEntry },
  turn_finished: { turn: finishedTurn },
  turn_failed: { turn: failedTurn },
  quota_exhausted: { failure: terminalFailure },
  queue_changed: { queue },
  prompt_state: { clientId: id, state: messageState, rev: seq },
  message_upsert: { message },
  background_upsert: { task: backgroundTask },
  background_removed: { id },
  shots_set: { shots: array(shot) },
  pictures_set: { pictures: array(picture) },
  feed_reset: { turns: array(turn), entries: array(entry), beforeTurnId: nullable(turnId) },
} satisfies Record<ConvEventType, Shape>;
const machineEvent: Decoder<MachineEvent> = variants('type', machineEventKinds, undefined, {
  machine_status: { health: machineHealth },
});
const convsEvent: Decoder<ConvsEvent> = variants('type', convsEventKinds);
const convEvent: Decoder<ConvEvent> = variants('type', convEventKinds);

const subShape = object(
  { f: constant('sub'), ch: channel },
  { epoch: id, afterSeq: int, afterEntryId: entryId, opts: object({}, { load: bool }) },
);
const sub: typeof subShape = (v, p) => {
  const frame = subShape(v, p);
  return frame.afterSeq !== undefined && frame.epoch === undefined
    ? fail(`${p}.afterSeq`, 'without an epoch')
    : frame;
};
const clientFrames = {
  hello: object({ f: constant('hello'), proto: constant(2), device: id, app: str }),
  sub,
  unsub: object({ f: constant('unsub'), ch: channel }),
  ping: object({ f: constant('ping'), t: int }),
} satisfies Record<ClientFrameKind, Decoder<ClientFrame>>;

const snapshot: Decoder<SnapshotFrame> = (v, p) => {
  const head = { f: constant('snapshot'), seq: int, mode: oneOf(SNAPSHOT_MODES) };
  const ch = record(v, p).ch;
  if (ch === 'machine') return object({ ...head, ch: constant('machine'), data: machineSnapshot })(v, p);
  if (ch === 'convs') return object({ ...head, ch: constant('convs'), data: convsSnapshot })(v, p);
  return object({ ...head, ch: convChannel, data: convSnapshot })(v, p);
};
const ev: Decoder<EvFrame> = (v, p) => {
  const head = { f: constant('ev'), seq };
  const ch = record(v, p).ch;
  if (ch === 'machine') return object({ ...head, ch: constant('machine'), e: machineEvent })(v, p);
  if (ch === 'convs') return object({ ...head, ch: constant('convs'), e: convsEvent })(v, p);
  return object({ ...head, ch: convChannel, e: convEvent })(v, p);
};
const serverFrames = {
  welcome: object({
    f: constant('welcome'),
    proto: constant(2),
    epoch: id,
    serverTime: ms,
    machine,
    features: array(feature),
    limits,
  }),
  snapshot,
  ev,
  pong: object({ f: constant('pong'), t: int }),
  error: object({ f: constant('error'), code: oneOf(FRAME_ERROR_CODES) }, { ch: channel }),
} satisfies Record<ServerFrameKind, Decoder<ServerFrame>>;

function byTag<T>(key: string, what: string, table: Record<string, Decoder<T>>): Decoder<T> {
  return (v, p) => {
    const tag = record(v, p)[key];
    if (typeof tag !== 'string' || !Object.hasOwn(table, tag)) {
      return fail(`${p}.${key}`, `unknown ${what} ${JSON.stringify(tag)}`);
    }
    return table[tag](v, p);
  };
}
const clientFrame = byTag<ClientFrame>('f', 'client frame', clientFrames);
const serverFrame = byTag<ServerFrame>('f', 'server frame', serverFrames);

const scoped = { opId: id, conversationId: convId };
const answerItem: Decoder<{ i: number; choices: number[]; text?: string }> = (v, p) => {
  const a = object({ i: int, choices: array(int) }, { text: id })(v, p);
  return (a.text !== undefined) !== a.choices.length > 0 ? a : fail(p, 'either choices or text');
};
const firstMessage: Decoder<FirstMessage> = object({ clientId: id, text: str, attachmentIds: array(id) });
const createBase = { opId: id, provider, model: nullable(model) };
const commandBodies: { [K in Op]: Decoder<CommandBodies[K]> } = {
  send: (v, p) => {
    const body = object(
      {
        opId: id,
        clientId: id,
        conversationId: convId,
        text: str,
        attachmentIds: array(id),
        mode: oneOf(MESSAGE_MODES),
        kind: oneOf(MESSAGE_KINDS),
      },
      { refs: messageRefs },
    )(v, p);
    return body.opId === body.clientId ? body : fail(`${p}.opId`, 'not the clientId');
  },
  cancel: object({ ...scoped, clientId: id }),
  retry: object({ ...scoped, clientId: id }),
  promote: object({ ...scoped, clientId: id, to: oneOf(PROMOTE_TOS) }),
  answer: byKey(
    'chatAbout',
    object({ ...scoped, questionId, chatAbout: constant(true) }),
    object({ ...scoped, questionId, answers: array(answerItem, 1) }),
  ),
  decide: object({ ...scoped, permissionId, optionId: id }),
  interrupt: object(scoped, { pauseQueue: bool }),
  'queue.resume': object(scoped),
  seen: object({
    opId: id,
    conversationId: (v, p) => (v === 'all' ? 'all' : convId(v, p)),
    upTo: ms,
  }),
  'conversation.create': byKey(
    'cwd',
    object({ ...createBase, cwd: id }, { first: firstMessage, worktree: bool, launch, profile: profileId }),
    object(
      { ...createBase, projectId: id },
      { first: firstMessage, worktree: bool, launch, profile: profileId },
    ),
  ),
  'conversation.rename': object({ opId: id, id: convId, title: str }),
  'conversation.retitle': object({ opId: id, id: convId }),
  'conversation.pin': object({ opId: id, id: convId, pinned: bool }),
  'conversation.move': object({ opId: id, id: convId, projectId: nullable(id) }),
  'conversation.delete': object({ opId: id, id: convId }, { force: bool }),
  'conversation.restore': object({ opId: id, id: convId }),
  'conversation.resume': byKey(
    'sessionId',
    object({ opId: id, provider, sessionId, cwd: absolutePath }),
    object({ opId: id, id: convId }),
  ),
  model: legacyUltracode(
    object({ opId: id, id: convId, model: id, effort: nullable(effort) }, { ultracode: bool }),
  ),
  'bg.stop': object({ ...scoped, taskId: id }),
  'project.create': object({ opId: id }, { path: id, name: id }),
  'project.update': object({ opId: id, id }, { path: id, name: id }),
  'project.delete': object({ opId: id, id, chats: oneOf(PROJECT_CHATS) }),
  'favorites.group.create': object({ opId: id, name: favName }),
  'favorites.group.rename': object({ opId: id, id: favGroupId, name: favName }),
  'favorites.group.delete': object({ opId: id, id: favGroupId }),
  'favorites.group.move': object({ opId: id, id: favGroupId, beforeId: nullable(favGroupId) }),
  'favorites.place': object({
    opId: id,
    id: convId,
    groupId: nullable(favGroupId),
    beforeId: nullable(convId),
  }),
  goal: object({ opId: id, id: convId, text: str }),
  'draft.set': object(
    { opId: id, key: draftKey, text: draftText, rev: seq },
    { attachments: draftFiles, refs: messageRefs },
  ),
  'queue.move': object({ ...scoped, clientId: id, beforeClientId: nullable(id) }),
  'terminal.stop': object({ opId: id, id: convId }, { force: bool }),
  rewind: object({ opId: id, id: convId, entryId: turnId, text: id, files: bool }),
  'rewind.undo': object({ opId: id, id: convId }),
  'conversation.fork': object({ opId: id, id: convId }, { atTurn: turnId }),
  'conversation.handoff': variants(
    'mode',
    { excerpt: { text: handoffText }, note: { text: handoffText }, history: { artifactId } },
    { opId: id, id: convId, provider, model: nullable(model) },
    { excerpt: { profile: profileId }, note: { profile: profileId }, history: { profile: profileId } },
  ),
  'conversation.merge': object({ opId: id, id: convId, text: handoffText }),
  limit: object({ opId: id, id: convId, action: oneOf(LIMIT_ACTIONS) }),
};

const effectShape = { ch: channel, seq };
const cmdResult: Decoder<CmdResult> = byTag<CmdResult>('r', 'result', {
  accepted: object({ r: constant('accepted'), ...effectShape }, { rev: seq, id }),
  queued: object({ r: constant('queued'), ...effectShape }, { rev: seq, id }),
  refused: object({ r: constant('refused'), code: oneOf(REFUSE_CODES) }),
} satisfies Record<ResultKind, Decoder<CmdResult>>);

/** Reads one frame of the runtime; anything outside the protocol is a ProtocolError. */
export function parseServerFrame(v: unknown): ServerFrame {
  return serverFrame(v, '$');
}

/** Reads one frame of a page, as a fake runtime in tests does. */
export function parseClientFrame(v: unknown): ClientFrame {
  return clientFrame(v, '$');
}

/** Reads a command as the runtime gets it: the op of the URL and the body. */
export function parseCommand(op: string, body: unknown): Command {
  if (!(OPS as readonly string[]).includes(op)) fail('$.op', `unknown op ${JSON.stringify(op)}`);
  return { op, body: (commandBodies[op as Op] as Decoder<unknown>)(body, '$.body') } as Command;
}

/** Reads the answer of POST /api/v2/cmd/<op>. */
export function parseCmdResult(v: unknown): CmdResult {
  return cmdResult(v, '$');
}

/** Reads the refusal a page sends instead of its answer. */
export function parseRefusal(v: unknown): Refusal {
  return object({ r: constant('refused'), code: oneOf(REFUSE_CODES) })(v, '$');
}

/** Reads the refusal of a designly page. */
export function parseDesignlyRefusal(v: unknown): DesignlyRefusal {
  return object({ r: constant('refused'), code: oneOf(REFUSE_CODES), message: id, projectId: nullable(id) })(
    v,
    '$',
  );
}

/** Reads the refusal of a stop of the Machine tool. */
export function parseMachineRefusal(v: unknown): MachineRefusal {
  return object({
    r: constant('refused'),
    code: oneOf(REFUSE_CODES),
    reason: oneOf(MACHINE_STOP_REASONS),
    message: id,
  })(v, '$');
}

/** Reads the refusal of a files page. */
export function parseFileRefusal(v: unknown): FileRefusal {
  return object({ r: constant('refused'), code: oneOf(REFUSE_CODES), message: id })(v, '$');
}

/** Readers of the answers of the v2 HTTP pages. */
const centralState: Decoder<CentralState> = oneOf(CENTRAL_STATES);
const centralList: Decoder<CentralList> = object(
  {
    enabled: bool,
    machines: array(object({ id, name: str, state: centralState })),
    conversations: array(conversation),
  },
  { moved: array(object({ machineId: id, convId, toMachineId: id, toConvId: convId })) },
);
const centralChat: Decoder<CentralChat> = object({
  machine: centralState,
  machineId: id,
  conversation,
  turns: array(turn),
  entries: array(entry),
  beforeTurnId: nullable(turnId),
});

const profileProject: Decoder<ProfileProject> = object({
  id,
  name: str,
  createdAt: ms,
  members: array(object({ machineId: id, projectId: id })),
});
const profileSidebar: Decoder<ProfileSidebar> = object({
  projects: array(profileProject),
  favorites,
  merged: array(id),
});

/** The gateway's answers about the person's chats on every machine: not runtime pages, so outside HttpKind. */
export const parseCentral = {
  list: (v: unknown): CentralList => centralList(v, '$'),
  chat: (v: unknown): CentralChat => centralChat(v, '$'),
  sidebar: (v: unknown): ProfileSidebarPage =>
    object({ enabled: bool, rev: int, doc: nullable(profileSidebar) })(v, '$'),
  sidebarWrite: (v: unknown): ProfileSidebarWrite =>
    object({ rev: int }, { conflict: bool, doc: nullable(profileSidebar) })(v, '$'),
  agentAccounts: (v: unknown): AgentAccountNote[] =>
    object({
      accounts: array(
        object({ machineId: id, provider: oneOf(PROVIDERS), who: id, firstAt: ms, lastAt: ms }),
      ),
    })(v, '$').accounts,
};

export const parsePage = {
  hello: (v: unknown): Hello => object({ proto: int, version: str, features: array(feature) })(v, '$'),
  turns: (v: unknown): TurnsPage =>
    object({ turns: array(turn), entries: array(entry), beforeTurnId: nullable(turnId) })(v, '$'),
  entries: (v: unknown): EntriesPage => object({ turnId, entries: array(entry) })(v, '$'),
  output: (v: unknown): OutputPage => object({ entryId, output: toolOutput })(v, '$'),
  upload: (v: unknown): AttachmentRef => attachmentRef(v, '$'),
  dirs: (v: unknown): Dir[] => array(dir)(v, '$'),
  sessions: (v: unknown): AgentSession[] => array(agentSession)(v, '$'),
  terminals: (v: unknown): Terminal[] => array(terminal)(v, '$'),
  terminal: (v: unknown): Terminal => terminal(v, '$'),
  voice: (v: unknown): Transcript => object({ text: str })(v, '$'),
  designlyCreate: (v: unknown): DesignlyCreated => object({ id, title: id, conv: convId })(v, '$'),
  designlyLibrary: (v: unknown): DesignLibrary =>
    object({ port: int, root: id, artifacts: array(designArtifact) })(v, '$'),
  designlySystems: (v: unknown): DesignSystemInfo[] => array(designSystemInfo)(v, '$'),
  designlyPin: (v: unknown): DesignlyPin => designlyPin(v, '$'),
  files: (v: unknown): DirListing =>
    object({ path: str, entries: array(fileEntry), truncated: bool })(v, '$'),
  file: (v: unknown): FileContent => object({ path: id, content: str, revision: id, bytes: int })(v, '$'),
  tasks: (v: unknown): Task[] => array(task)(v, '$'),
  legacy: (v: unknown): LegacyTab => legacyTab(v, '$'),
  open: (v: unknown): OpenTarget => openTarget(v, '$'),
  openRefs: (v: unknown): OpenedRefs => openedRefs(v, '$'),
  messageStatus: (v: unknown): MessageStatuses => object({ messages: array(messageStatus) })(v, '$'),
  context: (v: unknown): ConvContext => convContext(v, '$'),
  search: (v: unknown): MessageSearch => messageSearch(v, '$'),
  digest: (v: unknown): ChatDigest => chatDigest(v, '$'),
  machineStats: (v: unknown): MachineStats => machineStats(v, '$'),
  machineStop: (v: unknown): MachineStopped => machineStopped(v, '$'),
  apps: (v: unknown): AppsFeed => appsFeed(v, '$'),
  agentSettings: (v: unknown): AgentSettings => agentSettings(v, '$'),
  runtimeSettings: (v: unknown): RuntimeSettings => runtimeSettings(v, '$'),
  rewindPlan: (v: unknown): RewindPlan => rewindPlan(v, '$'),
  handoffDraft: (v: unknown): HandoffDraft => handoffDraft(v, '$'),
  profiles: (v: unknown): ProfileList => profileList(v, '$'),
  profileKey: (v: unknown): ProfileKey => profileKey(v, '$'),
  profileTest: (v: unknown): ProfileTest => profileTest(v, '$'),
  login: (v: unknown): LoginStatus => loginStatus(v, '$'),
  loginOK: (v: unknown): LoginOK => loginOK(v, '$'),
};
