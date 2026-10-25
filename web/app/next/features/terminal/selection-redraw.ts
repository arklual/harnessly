type TCell = [number, number];

interface ISelectionState {
  start: TCell | undefined;
  end: TCell | undefined;
  columnSelectMode: boolean;
}

/** The part of xterm 6.0's RenderService this module relies on; FitAddon reaches the same private service. */
export interface IRenderServiceInternals {
  _isPaused: boolean;
  _needsFullRefresh: boolean;
  _needsSelectionRefresh: boolean;
  _selectionState: ISelectionState;
  handleSelectionChanged: (
    start: TCell | undefined,
    end: TCell | undefined,
    columnSelectMode: boolean,
  ) => void;
}

const renderServiceOf = (terminal: unknown): IRenderServiceInternals | null => {
  const service = (terminal as { _core?: { _renderService?: Partial<IRenderServiceInternals> } })._core
    ?._renderService;
  if (!service) return null;
  if (
    typeof service.handleSelectionChanged !== 'function' ||
    typeof service._isPaused !== 'boolean' ||
    !service._selectionState
  )
    return null;
  return service as IRenderServiceInternals;
};

/**
 * xterm 6.0 refreshes the selection on every scroll, and its DOM renderer answers each refresh by repainting every
 * row — with no selection at all, and even while the renderer is paused off screen. A streaming agent scrolls
 * several times a second, so a hidden terminal kept repainting and a visible one painted everything twice.
 *
 * Without a selection before or after there is nothing to repaint. While paused, the selection is stored and drawn
 * by the full refresh xterm already runs when the terminal comes back on screen.
 *
 * TODO: drop with @xterm/xterm 6.1, whose DOM renderer only repaints the rows a selection touched.
 */
export const skipIdleSelectionRedraws = (terminal: unknown): boolean => {
  const service = renderServiceOf(terminal);
  if (!service) return false;
  const redraw = service.handleSelectionChanged.bind(service);
  service.handleSelectionChanged = (start, end, columnSelectMode) => {
    const state = service._selectionState;
    const hadSelection = !!(state.start && state.end);
    if (!hadSelection && !(start && end)) return;
    if (service._isPaused) {
      state.start = start;
      state.end = end;
      state.columnSelectMode = columnSelectMode;
      service._needsSelectionRefresh = true;
      service._needsFullRefresh = true;
      return;
    }
    redraw(start, end, columnSelectMode);
  };
  return true;
};
