// The kit's search (⌘K) in the look of the app's own search: results by kind (Компоненты, Макеты, Значки, Цвета…),
// ↑↓ and Enter, a pick flies the camera to the node. The same index an agent searches (kit-search.ts).
import { Dialog } from '@base-ui/react/dialog';
import { useMemo, useRef, useState, type KeyboardEvent } from 'react';
import type { IIndexEntry } from '@/next/features/designly/kit/kit-api';
import { groupResults, searchIndex } from '@/next/features/designly/kit/kit-search';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { t } from '@/next/i18n';

const KIND_ICON: Readonly<Record<string, string>> = {
  component: 'component',
  layout: 'presentation',
  template: 'layers',
  asset: 'image',
  color: 'palette',
  type: 'type',
  token: 'ruler',
};

interface IKitSearchDialogProps {
  open: boolean;
  index: readonly IIndexEntry[];
  onPick: (node: string) => void;
  onClose: () => void;
}

const KitSearchDialog = ({ open, index, onPick, onClose }: IKitSearchDialogProps) => {
  const container = usePortalContainer();
  const inputRef = useRef<HTMLInputElement | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);
  const [query, setQuery] = useState('');
  const [at, setAt] = useState(0);
  const groups = useMemo(() => groupResults(searchIndex(index, query)), [index, query]);
  const rows = useMemo(() => groups.flatMap((g) => g.entries), [groups]);

  const pick = (e: IIndexEntry | undefined) => {
    if (!e) return;
    onPick(e.node);
    onClose();
  };

  const move = (d: number) => {
    if (rows.length === 0) return;
    const next = (at + d + rows.length) % rows.length;
    setAt(next);
    listRef.current?.querySelector(`[data-row="${next}"]`)?.scrollIntoView({ block: 'nearest' });
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      move(1);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      move(-1);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      pick(rows[at]);
    }
  };

  let n = 0;
  return (
    <Dialog.Root
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <Dialog.Portal container={container}>
        <div className="hx-srch">
          <Dialog.Backdrop className="hx-srch__scrim" />
          <Dialog.Popup
            className="hx-srch__box"
            aria-label={t('next.kit.search.title')}
            initialFocus={inputRef}
          >
            <label className="hx-srch__field">
              <Icon name="search" />
              <input
                ref={inputRef}
                type="search"
                placeholder={t('next.kit.search.placeholder')}
                autoComplete="off"
                value={query}
                onChange={(e) => {
                  setAt(0);
                  setQuery(e.target.value);
                }}
                onKeyDown={onKeyDown}
              />
              <Dialog.Close className="hx-srch__cancel">{t('next.kit.search.cancel')}</Dialog.Close>
            </label>
            <div
              className="hx-srch__list"
              ref={listRef}
              role="listbox"
              aria-label={t('next.kit.search.results')}
            >
              {groups.map((g) => (
                <div
                  key={g.kind}
                  className="hx-srch__group"
                  role="group"
                  aria-label={t(`next.kit.kind.${g.kind}`)}
                >
                  <div className="hx-srch__label">{t(`next.kit.kind.${g.kind}`)}</div>
                  {g.entries.map((e) => {
                    const mine = n++;
                    return (
                      <button
                        key={e.node}
                        type="button"
                        role="option"
                        aria-selected={mine === at}
                        className="hx-srch__row"
                        data-row={mine}
                        data-sel={mine === at}
                        onClick={() => pick(e)}
                        onPointerMove={() => {
                          if (mine !== at) setAt(mine);
                        }}
                      >
                        <span className="hx-srch__ic">
                          <Icon name={KIND_ICON[e.kind] ?? 'circle'} size="sm" />
                        </span>
                        <span className="hx-srch__text">
                          <span className="hx-srch__title">{e.title}</span>
                          {e.path && <span className="hx-srch__snip">{e.path}</span>}
                        </span>
                        <Icon name="corner-down-left" size="sm" className="hx-srch__go" />
                      </button>
                    );
                  })}
                </div>
              ))}
              {query.trim() !== '' && rows.length === 0 && (
                <div className="hx-srch__none">{t('next.kit.search.none')}</div>
              )}
            </div>
          </Dialog.Popup>
        </div>
      </Dialog.Portal>
    </Dialog.Root>
  );
};

export default KitSearchDialog;
