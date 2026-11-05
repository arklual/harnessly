// Whether the right pane is out. On a chat's page that is the chat's kept state (stores/pane-state). Beside a design
// opened full screen it is the person's own choice there, starting shut: the chat's «open» is the chat's page's, and a
// pane that showed the design itself must not come out by itself when the design goes full screen («развернуть»).
import { useState } from 'react';

export const usePaneOpenBeside = (
  /** The chat's kept «open». */
  kept: boolean,
  /** Writes the chat's kept «open». */
  setKept: (next: boolean) => void,
  besideDesign: boolean,
  conv: string | null,
): [boolean, (next: boolean) => void] => {
  const [beside, setBeside] = useState(false);
  // Another design's chat, or the design left: the pane beside it starts shut again.
  const at = besideDesign ? conv : null;
  const [besideOf, setBesideOf] = useState(at);
  if (at !== besideOf) {
    setBesideOf(at);
    setBeside(false);
  }
  const open = besideDesign ? beside : kept;
  const setOpen = (next: boolean) => {
    if (besideDesign) setBeside(next);
    else setKept(next);
  };
  return [open, setOpen];
};
