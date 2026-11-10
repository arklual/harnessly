// «Спросить агента» over a selection (HRN-679): a small bar by the selected text — «Спросить агента», «Объясни»,
// «Исправь» — that puts the selection, with its path and lines, into the chat's field. It shows only once the mouse is
// let go (not while a selection is being dragged) and leaves with the selection.
import { StateEffect, StateField, type EditorState, type Extension } from '@codemirror/state';
import { EditorView, showTooltip, type Tooltip } from '@codemirror/view';

export interface IAskChoice {
  label: string;
  /** What the agent is asked to do with it; null leaves the question to the person. */
  note: string | null;
  /** The first choice carries the chat's icon. */
  icon?: boolean;
}

/** The pointer is down in the editor: the bar waits for it to be let go. */
const dragging = StateEffect.define<boolean>();

const tipOf = (
  state: EditorState,
  choices: () => readonly IAskChoice[],
  onAsk: (view: EditorView, note: string | null) => void,
): Tooltip | null => {
  const range = state.selection.main;
  if (range.empty) return null;
  return {
    pos: range.head,
    above: range.head <= range.anchor,
    arrow: false,
    create: (view) => {
      const dom = document.createElement('div');
      dom.className = 'hx-asktip';
      dom.setAttribute('role', 'toolbar');
      for (const choice of choices()) {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'hx-asktip__btn';
        if (choice.icon) {
          const icon = document.createElement('i');
          icon.className = 'hx-icon';
          icon.dataset.i = 'message-square-text';
          icon.dataset.size = 'sm';
          icon.setAttribute('aria-hidden', 'true');
          button.append(icon);
        }
        button.append(choice.label);
        // The selection stays: a press on the bar does not take the focus or the selection away.
        button.addEventListener('mousedown', (event) => event.preventDefault());
        button.addEventListener('click', () => onAsk(view, choice.note));
        dom.append(button);
      }
      return { dom };
    },
  };
};

/** The bar as an extension: `choices` are read when it shows, `onAsk` gets the view and the note picked. */
export const askTip = (
  choices: () => readonly IAskChoice[],
  onAsk: (view: EditorView, note: string | null) => void,
): Extension => {
  const field = StateField.define<{ down: boolean; tip: Tooltip | null }>({
    create: () => ({ down: false, tip: null }),
    update: (value, tr) => {
      let down = value.down;
      for (const effect of tr.effects) if (effect.is(dragging)) down = effect.value;
      if (down) return { down, tip: null };
      if (!tr.selection && !tr.docChanged && down === value.down) return value;
      return { down, tip: tipOf(tr.state, choices, onAsk) };
    },
    provide: (f) => showTooltip.compute([f], (state) => state.field(f).tip),
  });
  return [
    field,
    EditorView.domEventHandlers({
      mousedown: (_event, view) => {
        view.dispatch({ effects: dragging.of(true) });
        const up = () => {
          document.removeEventListener('mouseup', up);
          view.dispatch({ effects: dragging.of(false) });
        };
        document.addEventListener('mouseup', up);
        return false;
      },
    }),
  ];
};
