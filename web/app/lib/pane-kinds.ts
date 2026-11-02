/**
 * The kinds of tab the right pane holds: one registry, with the runtime's (internal/panel/kinds.go) holding the same
 * names. An installed app adds no kind of its own: it opens in the `app` kind, whose target is `<app id>[/<view>]`, so
 * a new app needs no change here.
 */
export const PANE_TAB_KINDS = [
  'overview',
  'tools',
  'browser',
  'terminal',
  'files',
  'disk',
  'changes',
  'artifacts',
  'gitlab',
  'subagents',
  'economics',
  'plan',
  'peers',
  'design',
  'side-chat',
  'gallery',
  'task',
  'vault',
  'app',
] as const;
export type TPaneTabKind = (typeof PANE_TAB_KINDS)[number];

/** One of these per conversation: the target of a browser is where it goes, not which browser it is. */
export const SINGLE_PANE_KINDS: readonly TPaneTabKind[] = [
  'overview',
  'tools',
  'browser',
  'terminal',
  'vault',
];
