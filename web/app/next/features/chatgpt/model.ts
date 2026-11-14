// What the ChatGPT integration answers (internal/chatgpt): accounts without tokens, models of the plan, conversations.

export interface IGptAccount {
  key: string;
  /** The account's e-mail, shown on the person's own page only. */
  label: string;
  plan?: string;
  signedIn: boolean;
  /** The person allowed harnessly to use their ChatGPT plan. */
  planUse: boolean;
  active: boolean;
}

export interface IGptState {
  accounts: IGptAccount[] | null;
  pending: boolean;
}

export interface IGptModel {
  slug: string;
  name: string;
  efforts?: string[];
  defaultEffort?: string;
  images: boolean;
  files: boolean;
}

export interface IGptFile {
  id: string;
  name: string;
  mime: string;
  size: number;
}

export const GPT_STATUS = { done: 'done', stopped: 'stopped', failed: 'failed' } as const;
export type GptStatus = (typeof GPT_STATUS)[keyof typeof GPT_STATUS];

export interface IGptMessage {
  id: string;
  role: 'user' | 'assistant';
  text: string;
  files?: IGptFile[];
  at: number;
  model?: string;
  status?: GptStatus;
  error?: { code: string; detail?: string };
}

export interface IGptChat {
  id: string;
  title: string;
  model?: string;
  effort?: string;
  createdAt: number;
  updatedAt: number;
  messages: IGptMessage[] | null;
}

export interface IGptChatSummary {
  id: string;
  title: string;
  model?: string;
  createdAt: number;
  updatedAt: number;
  running: boolean;
}

export interface IGptUpload {
  name: string;
  mime: string;
  /** base64 */
  data: string;
}

/** The kinds of files the plan's models take in a message. */
export const GPT_IMAGE_TYPES: readonly string[] = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];
export const GPT_FILE_TYPES: readonly string[] = [
  'application/pdf',
  'text/plain',
  'text/markdown',
  'text/csv',
  'application/json',
];

/** The words for a refusal code of the machine, a key of the dictionary. */
const REFUSALS: Readonly<Record<string, string>> = {
  'signed-out': 'next.chatgpt.err.signedOut',
  'no-account': 'next.chatgpt.err.signedOut',
  'no-plan': 'next.chatgpt.err.noPlan',
  'usage-limit': 'next.chatgpt.err.usageLimit',
  'not-eligible': 'next.chatgpt.err.notEligible',
  unavailable: 'next.chatgpt.err.unavailable',
  unsupported: 'next.chatgpt.err.unsupported',
  network: 'next.chatgpt.err.network',
  busy: 'next.chatgpt.err.busy',
  incomplete: 'next.chatgpt.err.incomplete',
  denied: 'next.chatgpt.err.denied',
  state: 'next.chatgpt.err.state',
  'bad-callback': 'next.chatgpt.err.badCallback',
  'account-mismatch': 'next.chatgpt.err.accountMismatch',
  'too-many': 'next.chatgpt.err.tooMany',
};

export const refusalKey = (code: string): string => REFUSALS[code] ?? 'next.chatgpt.err.other';

/** Where the person sees and limits what harnessly spends of their plan. */
export const USAGE_URL = 'https://chatgpt.com/settings/usage';
