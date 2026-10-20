import { PROVIDER_LABELS, type TProviderStatus } from '@/components/agents/provider-icon';
import { compactAge, compactAgeIsNow } from '@/lib/hl-compact-time';
import { STATUS_TONE, statusTone, type TStatusTone } from '@/lib/hl-status-tone';
import { t } from '@/lib/i18n';

export { STATUS_TONE, statusTone, type TStatusTone };

export const RAIL_TOOLTIP_KIND = {
  action: 'action',
  conversation: 'conversation',
  project: 'project',
} as const;

/** Dictionary keys of the status words; idle has none. */
const STATUS_KEYS: Record<TProviderStatus, string | null> = {
  idle: null,
  working: 'lib.hlRailTooltip.statusWorking',
  'needs-input': 'lib.hlRailTooltip.statusNeedsInput',
  ready: 'lib.hlRailTooltip.statusReady',
  done: 'lib.hlRailTooltip.statusDone',
  failed: 'lib.hlRailTooltip.statusFailed',
  gone: 'lib.hlRailTooltip.statusOffline',
};

const statusWord = (status: TProviderStatus): string => {
  const key = STATUS_KEYS[status];
  return key ? t(key) : '';
};

export interface IRailFact {
  label: string;
  value: string;
}

export interface IRailTooltipStatus {
  tone: TStatusTone;
  label: string;
}

export interface IRailTooltip {
  title: string;
  description?: string;
  shortcut?: string;
  status: IRailTooltipStatus | null;
  facts: IRailFact[];
  /** The same content as one plain sentence: the trigger's accessible description. */
  text: string;
}

export type TRailTooltipInput =
  | {
      kind: typeof RAIL_TOOLTIP_KIND.action;
      title: string;
      description?: string;
      shortcut?: string;
      facts?: IRailFact[];
    }
  | {
      kind: typeof RAIL_TOOLTIP_KIND.conversation;
      title: string;
      provider: string;
      status: TProviderStatus;
      machineName?: string | null;
      updatedAt?: number;
      shortcut?: string;
      /** Why the conversation cannot be reached right now (machine away, chat removed). */
      unavailable?: string;
    }
  | {
      kind: typeof RAIL_TOOLTIP_KIND.project;
      name: string;
      count: number;
      summary: { status: TProviderStatus; count: number } | null;
    };

const agoText = (at: number, now: number): string => {
  if (compactAgeIsNow(at, now)) return t('lib.hlRailTooltip.justNow');
  return t('lib.hlRailTooltip.ago', { age: compactAge(at, now) });
};

const sentence = (tip: Omit<IRailTooltip, 'text'>): string =>
  [tip.title, tip.status?.label, tip.description, ...tip.facts.map((fact) => `${fact.label}: ${fact.value}`)]
    .filter(Boolean)
    .join('. ')
    .concat(tip.shortcut ? `. ${tip.shortcut}` : '');

const finish = (tip: Omit<IRailTooltip, 'text'>): IRailTooltip => ({ ...tip, text: sentence(tip) });

const conversationTooltip = (
  input: Extract<TRailTooltipInput, { kind: 'conversation' }>,
  now: number,
): IRailTooltip => {
  const offline = Boolean(input.unavailable) || input.status === 'gone';
  const tone = offline ? STATUS_TONE.offline : statusTone(input.status);
  const facts: IRailFact[] = [
    { label: t('lib.hlRailTooltip.agent'), value: PROVIDER_LABELS[input.provider] ?? input.provider },
  ];
  if (input.machineName) facts.push({ label: t('lib.hlRailTooltip.machine'), value: input.machineName });
  if (input.updatedAt)
    facts.push({ label: t('lib.hlRailTooltip.activity'), value: agoText(input.updatedAt, now) });
  return finish({
    title: input.title,
    description:
      input.unavailable ?? (input.status === 'gone' ? t('lib.hlRailTooltip.processStopped') : undefined),
    shortcut: input.shortcut,
    status: tone
      ? { tone, label: offline ? t('lib.hlRailTooltip.statusOffline') : statusWord(input.status) }
      : null,
    facts,
  });
};

const projectTooltip = (input: Extract<TRailTooltipInput, { kind: 'project' }>): IRailTooltip => {
  const tone = input.summary ? statusTone(input.summary.status) : null;
  return finish({
    title: input.name,
    description: t('lib.hlRailTooltip.project'),
    status:
      tone && input.summary
        ? { tone, label: `${statusWord(input.summary.status)}: ${input.summary.count}` }
        : null,
    facts: [
      {
        label: t('lib.hlRailTooltip.conversations'),
        value: input.count > 0 ? String(input.count) : t('lib.hlRailTooltip.none'),
      },
    ],
  });
};

/** `now` is read by conversations only (their last activity is relative to it). */
export const railTooltipOf = (input: TRailTooltipInput, now = 0): IRailTooltip => {
  if (input.kind === RAIL_TOOLTIP_KIND.conversation) return conversationTooltip(input, now);
  if (input.kind === RAIL_TOOLTIP_KIND.project) return projectTooltip(input);
  return finish({
    title: input.title,
    description: input.description,
    shortcut: input.shortcut,
    status: null,
    facts: input.facts ?? [],
  });
};
