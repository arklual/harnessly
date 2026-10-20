export const TERMINAL_RATIO_DEFAULT = 30;

export type TTerminalSplitLayout = Record<'timeline' | 'terminal-area', number>;

const validRatio = (ratio: number | undefined): number =>
  ratio !== undefined && Number.isFinite(ratio) && ratio > 0 && ratio < 100 ? ratio : TERMINAL_RATIO_DEFAULT;

export const terminalSplitLayout = (
  isAgentPanel: boolean,
  collapsed: boolean,
  ratio?: number,
): TTerminalSplitLayout => {
  if (!isAgentPanel) return { timeline: 0, 'terminal-area': 100 };
  if (collapsed) return { timeline: 100, 'terminal-area': 0 };
  const terminal = validRatio(ratio);
  return { timeline: 100 - terminal, 'terminal-area': terminal };
};
