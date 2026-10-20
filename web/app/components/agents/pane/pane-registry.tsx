import { appConfig } from '@/platform/config';
import type { ComponentType } from 'react';
import {
  AppWindow,
  Bot,
  FileCode2,
  Gauge,
  GitCompareArrows,
  GitMerge,
  HardDrive,
  Images,
  ClipboardList,
  KeyRound,
  LayoutList,
  LayoutTemplate,
  ListChecks,
  MessageCirclePlus,
  MessagesSquare,
  Plus,
  Sparkles,
  SquareTerminal,
} from 'lucide-react';
import type { TPaneTabKind } from '@/lib/hl-pane-tabs';
import { t } from '@/lib/i18n';

export { openPaneTab } from '@/lib/hl-pane-store';

interface IPaneKindSpec {
  title: string;
  icon: ComponentType<{ className?: string }>;
  /** Needs a conversation with a session on a machine: a plain chat has no terminal, no dev servers, no diff. */
  agentOnly: boolean;
}

export const PANE_KINDS: Record<TPaneTabKind, IPaneKindSpec> = {
  overview: { title: t('pane.paneRegistry.overview'), icon: LayoutList, agentOnly: false },
  tools: { title: t('pane.paneRegistry.tools'), icon: Plus, agentOnly: false },
  browser: { title: t('pane.paneRegistry.browser'), icon: AppWindow, agentOnly: true },
  terminal: { title: t('pane.paneRegistry.terminal'), icon: SquareTerminal, agentOnly: true },
  'side-chat': { title: t('pane.paneRegistry.sideChat'), icon: MessageCirclePlus, agentOnly: false },
  files: { title: t('pane.paneRegistry.files'), icon: FileCode2, agentOnly: true },
  disk: { title: t('pane.paneRegistry.disk'), icon: HardDrive, agentOnly: true },
  changes: { title: t('pane.paneRegistry.changes'), icon: GitCompareArrows, agentOnly: true },
  artifacts: { title: t('pane.paneRegistry.artifacts'), icon: Sparkles, agentOnly: false },
  gitlab: { title: 'GitLab', icon: GitMerge, agentOnly: true },
  subagents: { title: t('pane.paneRegistry.subagents'), icon: Bot, agentOnly: true },
  plan: { title: t('pane.paneRegistry.plan'), icon: ListChecks, agentOnly: false },
  peers: { title: t('pane.paneRegistry.peers'), icon: MessagesSquare, agentOnly: true },
  economics: { title: t('pane.paneRegistry.economics'), icon: Gauge, agentOnly: false },
  design: { title: 'Claude Design', icon: LayoutTemplate, agentOnly: true },
  gallery: { title: t('pane.paneRegistry.gallery'), icon: Images, agentOnly: false },
  task: { title: t('pane.paneRegistry.task'), icon: ClipboardList, agentOnly: false },
  vault: { title: t('pane.paneRegistry.vault'), icon: KeyRound, agentOnly: false },
};

/** The «Tools» page in this order: Codex's own tools first, then ours. */
const ALL_TOOLS: readonly TPaneTabKind[] = [
  'side-chat',
  'terminal',
  'browser',
  'files',
  'disk',
  'changes',
  'artifacts',
  'gitlab',
  'subagents',
  'plan',
  'peers',
  'economics',
  'design',
  'gallery',
  'vault',
];

/** The tools this installation offers: Claude Design and the safe only where the page config turns them on. */
export const TOOL_ORDER: readonly TPaneTabKind[] = ALL_TOOLS.filter(
  (kind) =>
    (kind !== 'design' || appConfig().features.design) && (kind !== 'vault' || appConfig().features.vault),
);

/** `aria-keyshortcuts` of the tools with a chord: both spellings, the page cannot know which keyboard is used. */
export const PANE_ARIA_SHORTCUTS: Partial<Record<TPaneTabKind, string>> = {
  'side-chat': 'Alt+Meta+S Control+Alt+S',
  terminal: 'Control+`',
};

/** `userAgentData` where the browser has it (Chromium), `platform` elsewhere (Safari, Firefox). */
const isMacPlatform = (): boolean => {
  if (typeof navigator === 'undefined') return false;
  const platform =
    (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ||
    navigator.platform;
  return /mac|iphone|ipad/i.test(platform);
};

/** The chord as the person types it here: ⌥⌘S on a Mac, Ctrl+Alt+S elsewhere (see `paneShortcutFromEvent`). */
export const paneShortcutLabel = (kind: TPaneTabKind): string | null => {
  if (kind === 'side-chat') return isMacPlatform() ? '⌥⌘S' : 'Ctrl+Alt+S';
  if (kind === 'terminal') return isMacPlatform() ? '⌃`' : 'Ctrl+`';
  return null;
};
