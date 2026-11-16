// The header's buttons that go into the right pane's strip while the pane is open (it takes the window's whole height,
// so they cannot stay over it): the agent's terminal and «В фоне». The same buttons stand in the header when it is shut.
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

export interface IHeaderBackground {
  /** Subagents and background commands still running, and all of them; the button is there while there is any, or a goal. */
  running: number;
  total: number;
  recap: boolean;
  /** Something put away from the smart notice still waits: its drop is in the column. */
  waits?: boolean;
  /** Nothing in the column but the person's widgets: the button shows and hides them. */
  widgets?: boolean;
  /** The widgets stand as a column beside the chat (room enough), not a popover at the button: open is the layout's
   * own state then, so the button keeps no pressed ground for it. */
  docked?: boolean;
  open: boolean;
  onToggle: () => void;
}

export interface IHeaderToggle {
  open: boolean;
  onToggle: () => void;
}

/** What the column's button says on hover: what runs, that it has finished, that there is a recap, or only the pictures the agent looked at. */
const backgroundTip = (background: IHeaderBackground): string => {
  if (background.waits) return t('next.smart.drops.title');
  if (background.running > 0) return t('next.header.backgroundRunning', { n: background.running });
  if (background.total > 0) return t('next.header.backgroundDone');
  if (background.recap) return t('next.header.recap');
  return t('next.header.shots');
};

export const TerminalButton = ({ terminal }: { terminal: IHeaderToggle }) => (
  <button
    type="button"
    className="hx-head-btn hx-tipped hx-header__term"
    aria-pressed={terminal.open}
    aria-label={t('next.term.label')}
    onClick={terminal.onToggle}
  >
    <Icon name="square-terminal" size="sm" />
    <span className="hx-tip" data-pos="below" data-align="end">
      {t('next.term.label')}
      <kbd>⌃</kbd>
      <kbd>`</kbd>
    </span>
  </button>
);

export const BackgroundButton = ({ background }: { background: IHeaderBackground }) => {
  // Only the person's widgets: a plain round header button like the terminal's and the pane's, a grid of cards.
  if (background.widgets) {
    return (
      <div className="hx-bgwrap">
        <button
          type="button"
          className="hx-head-btn hx-bg-btn hx-wg-btn hx-tipped"
          aria-pressed={background.open}
          data-docked={background.docked || undefined}
          aria-label={t('next.widgets.title')}
          onClick={background.onToggle}
        >
          <Icon name="layout-grid" size="sm" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t('next.widgets.title')}
          </span>
        </button>
      </div>
    );
  }
  return (
    <div className="hx-bgwrap">
      <button
        type="button"
        className="hx-head-btn hx-bg-btn hx-tipped"
        aria-pressed={background.open}
        data-run={background.running > 0}
        aria-label={t('next.header.background')}
        onClick={background.onToggle}
      >
        <Icon name="layers" size="sm" />
        <span className="hx-bg-btn__n">{background.running > 0 ? background.running : ''}</span>
        {background.waits && !background.open && <span className="hx-bg-btn__wait" aria-hidden="true" />}
        <span className="hx-tip" data-pos="below" data-align="end">
          {backgroundTip(background)}
        </span>
      </button>
    </div>
  );
};
