interface IMeasurable {
  isConnected: boolean;
  clientWidth: number;
  clientHeight: number;
}

interface IStyled {
  style: { transform: string };
}

/**
 * Moves a hidden xterm out of the viewport without touching its layout box.
 *
 * xterm pauses its renderer while its screen does not intersect the viewport (the same observer VS Code relies on),
 * but `visibility: hidden` and a collapsed panel both leave the screen intersecting. A transform changes neither the
 * container size FitAddon measures nor the glyph size xterm measured, so cols/rows — and the pty width behind them —
 * stay exactly what they were, while the parser keeps consuming output and firing title changes.
 */
export const HIDDEN_TERMINAL_TRANSFORM = 'translateX(-200vw)';

export const setTerminalOffscreen = (element: IStyled, hidden: boolean) => {
  const next = hidden ? HIDDEN_TERMINAL_TRANSFORM : '';
  if (element.style.transform !== next) element.style.transform = next;
};

/**
 * FitAddon turns an unlaid-out container into a 2×1 terminal and the pty follows, reflowing every TUI in it.
 * A container without a box (display: none, detached, zero-sized) has nothing to measure.
 */
export const canFitTerminal = (container: IMeasurable | null | undefined): container is IMeasurable =>
  !!container && container.isConnected && container.clientWidth > 0 && container.clientHeight > 0;

interface IFitAddonLike {
  fit: () => void;
}

interface IGrid {
  cols: number;
  rows: number;
}

/** Refits the grid to its container; null when there is nothing to measure, so no size may reach the pty. */
export const fitToContainer = (
  container: IMeasurable | null | undefined,
  fitAddon: IFitAddonLike,
  grid: IGrid,
): IGrid | null => {
  if (!canFitTerminal(container)) return null;
  fitAddon.fit();
  return { cols: grid.cols, rows: grid.rows };
};
