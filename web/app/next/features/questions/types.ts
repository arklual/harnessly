// The agent's gates as the questions view reads them: a copy of Question, Permission and GateState of protocol/v2.ts
// (re-exports once it is on this branch).
import type { EntryId, Ms } from '@/next/features/chat/types';

export type GateCloseReason = 'agent-moved-on' | 'chat-about' | 'turn-ended' | 'agent-gone';

export type GateRefusal = 'menu-unmatched';

export type GateState =
  | { k: 'open'; refused?: GateRefusal }
  | { k: 'answering'; opId: string }
  | { k: 'answered'; at: Ms; byDevice: string }
  | { k: 'closed'; reason: GateCloseReason };

export interface QuestionItem {
  header: string;
  question: string;
  multi: boolean;
  options: { label: string; description: string | null; preview: string | null }[];
}

export interface Question {
  id: string;
  turnId: EntryId;
  askedAt: Ms;
  items: QuestionItem[];
  state: GateState;
}

export type PermissionScope = 'once' | 'always' | 'deny' | 'plan-auto' | 'plan-ask' | 'revise';

export interface Permission {
  id: string;
  kind: 'tool' | 'plan';
  askedAt: Ms;
  tool: string | null;
  command: string | null;
  cwd: string | null;
  planMd: string | null;
  options: { id: string; label: string; scope: PermissionScope }[];
  state: GateState;
}

/** One answer of the answer command: the chosen options of question i, or the person's own text. */
export interface AnswerItem {
  i: number;
  choices: number[];
  text?: string;
}
