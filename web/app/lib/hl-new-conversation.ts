import type { IMachineFeatures, TMachineFeature } from '@/types/server/hl-machines';
import type { ILaunchChoice } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

/** What "New conversation" can start. */
export const NEW_CONVERSATION_KINDS = ['claude', 'codex', 'chat'] as const;
export type TNewConversationKind = (typeof NEW_CONVERSATION_KINDS)[number];

export const isNewConversationKind = (value: unknown): value is TNewConversationKind =>
  typeof value === 'string' && (NEW_CONVERSATION_KINDS as readonly string[]).includes(value);

export interface INewConversationOption {
  kind: TNewConversationKind;
  title: string;
  hint: string;
  /** Shown only on a machine whose `features` has this flag. */
  requires?: TMachineFeature;
}

export const NEW_CONVERSATION_OPTIONS = [
  { kind: 'claude', title: 'Claude', hint: t('lib.hlNewConversation.claudeHint') },
  { kind: 'codex', title: 'Codex', hint: t('lib.hlNewConversation.codexHint') },
  { kind: 'chat', title: t('lib.hlNewConversation.chatTitle'), hint: t('lib.hlNewConversation.chatHint') },
] as const satisfies readonly INewConversationOption[];

/** Options for the machine new conversations go to; `null` features (loading, unknown machine) hide every gated option. */
export const newConversationOptions = (
  features: IMachineFeatures | null | undefined,
): readonly INewConversationOption[] =>
  NEW_CONVERSATION_OPTIONS.filter(
    (option: INewConversationOption) => !option.requires || features?.[option.requires] === true,
  );

/** How the shell starts a conversation: `start(kind, prompt, options)` in workspace.tsx. */
export interface IStartOptions {
  /** Model and effort from the welcome chip; absent means "Auto". */
  launch?: ILaunchChoice;
  /** A slash command sent by the composer once the agent is ready instead of a positional prompt. */
  firstLine?: string;
  /** Organizer project the new conversation joins after it is created. */
  projectId?: string;
  /** Runtime workspace (directory) the agent starts in; absent means the machine's default one. */
  workspaceId?: string;
}
