// What the dock under the feed draws, as plain props: the composer's attachments and model, the queue's rows, the
// notices over the field. Message and queue shapes follow protocol/v2.ts (see features/chat/types.ts).
import type { AttachmentRef, LocalMessageState, Ms, Provider, QueuePause } from '@/next/features/chat/types';
import type { MessageRef } from '@/next/protocol/v2';

export type { QueuePause };

export interface ComposerAttachment {
  id: string;
  name: string;
  kind: 'image' | 'file';
  status: 'uploading' | 'ready' | 'error';
  /** Why it cannot go: empty, wrong type, too large. */
  error: string | null;
}

export interface ComposerModel {
  provider: Provider;
  name: string;
  effort: string | null;
}

export interface ComposerContext {
  id: string;
  label: string;
  icon: 'folder' | 'list-checks' | 'image' | 'key-round';
}

/** How the field is used now; it changes the placeholder. */
/** stopped: the agent is not running; a message resumes it where it left off. */
export type ComposerMode = 'new' | 'idle' | 'stopped' | 'working' | 'answer';

export interface QueueRow {
  clientId: string;
  text: string;
  attachments: AttachmentRef[];
  /** The places it points at, while the runtime has not written their block into the text yet. */
  refs?: MessageRef[];
  state: LocalMessageState;
  /** Typed into the working agent's turn (Now / ⌃↵), not left for the end of it. */
  steer?: boolean;
  /** Sent «now»: it stays ahead of the messages that merely wait, whoever is carried. */
  ahead?: boolean;
  /** Typed in, and the agent has not read it yet: it reads it after its current step. */
  handed?: boolean;
}

/** A one-line notice over the field: the machine stopped answering, or the subscription's limit ran out. */
export type DockNotice =
  /** The machine of the chat is switched off: the chat is a read-only copy. */
  | { kind: 'off' }
  | { kind: 'machine'; machine: string; onStop: () => Promise<unknown> }
  /** The key of the chat's vendor was removed: it can be added again or the chat continued in another model. */
  | { kind: 'key'; vendor: string; onOpen: () => void; onContinue: (() => void) | null }
  | {
      kind: 'limit';
      provider: Provider;
      until: Ms;
      /** The person chose to wait: messages go by themselves when the limit is back. */
      waiting: boolean;
      /** A model to switch to (e.g. «Sonnet 5.5»), null when there is none. */
      switchTo: string | null;
      onSwitch: () => Promise<unknown>;
      onUseKey: (() => Promise<unknown>) | null;
      onWait: () => void;
    };

export interface Recording {
  startedAt: Ms;
  /** Stopped: the machine is turning it into text. */
  busy: boolean;
  /** The machine is downloading its speech model (the first dictation on it): the words come once it is there. */
  preparing?: boolean;
  /** The microphone's loudness now, 0..1, read once per frame; null when the browser gave no meter. */
  level: (() => number) | null;
}
