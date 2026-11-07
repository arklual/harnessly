// «Применить на машинах» (docs/hub/environment.md, «Apply on the machines»): the layer's managers ask the machines it
// reaches to bring the list and the base package up to date — now, or when each machine next starts.
import { useRef, useState } from 'react';

import type { ApplyWhen } from '../../../../hub-api/env';
import { Icon, usePop } from '../../ui';

const WHEN: { when: ApplyWhen; title: string; hint: string }[] = [
  { when: 'now', title: 'Сейчас', hint: 'машины в сети — сразу, остальные — когда включатся' },
  { when: 'next', title: 'При следующем запуске', hint: 'не трогать тех, кто работает сейчас' },
];

export const ApplyMenu = ({ onApply }: { onApply: (when: ApplyWhen) => Promise<boolean> }) => {
  const root = useRef<HTMLSpanElement>(null);
  const pop = usePop(root);
  const [busy, setBusy] = useState(false);
  return (
    <span className="ev-move ev-apply" ref={root}>
      <button
        type="button"
        className="hx-btn"
        data-size="sm"
        aria-label="Применить на машинах"
        aria-haspopup="listbox"
        aria-expanded={pop.open ? 'true' : 'false'}
        disabled={busy}
        onClick={pop.toggle}
      >
        <Icon i="rotate-cw" size="sm" />
        <span className="ev-tl">Применить на машинах</span>
      </button>
      {pop.mounted && (
        <div
          className="hb-pop ev-move__pop ev-apply__pop"
          data-closing={pop.attr}
          role="listbox"
          aria-label="Когда применить"
        >
          {WHEN.map((w) => (
            <button
              type="button"
              className="hb-pop__item"
              role="option"
              aria-selected="false"
              key={w.when}
              onClick={() => {
                pop.close();
                setBusy(true);
                void onApply(w.when).finally(() => setBusy(false));
              }}
            >
              <span>{w.title}</span>
              <small>{w.hint}</small>
            </button>
          ))}
        </div>
      )}
    </span>
  );
};
