// «Инструменты» (.hx-tl): what the pane can open, in groups of tiles, three across a usual pane (the design's grid) — an
// icon, the name, what the tool holds now under it when there is something to say (3 running, 2 open) and its shortcut,
// quietly at the end; the line about the tool is its tooltip. The search takes an address too: ↑↓ walk the tiles, Enter
// opens the one under the cursor (the first found while typing), or the address in the browser.
import { useEffect, useId, useState } from 'react';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import { parseAddress, type IAddress } from '@/next/features/pane/address';
import { t } from '@/next/i18n';

export interface ITool {
  id: string;
  group: string;
  icon: string;
  name: string;
  hint: string;
  kbd?: string;
  /** What the tool holds right now (3 running, 2 open): a line under the name while there is something to say. */
  live?: string;
}

export interface IToolGroup {
  id: string;
  label: string;
}

export interface IToolsLauncherProps {
  tools: ITool[];
  groups: IToolGroup[];
  onOpen: (id: string) => void;
  onAddress: (address: IAddress) => void;
}

/** The rows come in one after another; past this many they come together. */
const STAGGER_MAX = 11;

const ToolsLauncher = ({ tools, groups, onOpen, onAddress }: IToolsLauncherProps) => {
  const [query, setQuery] = useState('');
  const [cursor, setCursor] = useState<number | null>(null);
  const listId = useId();
  const q = query.trim().toLowerCase();
  const found = tools.filter((x) => !q || `${x.name} ${x.hint}`.toLowerCase().includes(q));
  const address = parseAddress(query);
  const shown = groups
    .map((g) => ({ ...g, items: found.filter((x) => x.group === g.id) }))
    .filter((g) => g.items.length > 0);
  // The rows in the order they are drawn: the cursor walks this.
  const flat = shown.flatMap((g) => g.items);
  // While typing the first found is the one Enter opens; the arrows take over from there.
  const at = cursor !== null ? Math.min(cursor, flat.length - 1) : q ? 0 : -1;
  const current = at >= 0 ? flat[at] : undefined;
  const rowId = (id: string) => `${listId}-${id}`;

  useEffect(() => {
    if (!current) return;
    document.getElementById(rowId(current.id))?.scrollIntoView({ block: 'nearest' });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the row to bring into view is named by its id
  }, [current?.id]);

  const walk = (step: 1 | -1) => {
    if (flat.length === 0) return;
    const from = at < 0 ? (step === 1 ? -1 : 0) : at;
    setCursor((from + step + flat.length) % flat.length);
  };

  return (
    <div className="hx-tl">
      <label className="hx-tl__search">
        <Icon name="search" size="sm" />
        <input
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls={listId}
          aria-activedescendant={current ? rowId(current.id) : undefined}
          autoComplete="off"
          autoFocus
          placeholder={t('next.pane.toolsSearch')}
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setCursor(null);
          }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
              event.preventDefault();
              walk(event.key === 'ArrowDown' ? 1 : -1);
              return;
            }
            if (event.key === 'Escape' && query !== '') {
              // The first Esc clears the search; the pane's own Esc is for an empty field.
              event.preventDefault();
              event.stopPropagation();
              setQuery('');
              setCursor(null);
              return;
            }
            if (event.key !== 'Enter') return;
            event.preventDefault();
            if (address && cursor === null) onAddress(address);
            else if (current) onOpen(current.id);
          }}
        />
        {!q && <kbd className="hx-tl__hint">↑↓</kbd>}
      </label>
      {address && (
        <button type="button" className="hx-tl__url" onClick={() => onAddress(address)}>
          <span className="hx-tl__ico">
            <Icon name="globe" />
          </span>
          <span className="hx-tl__name">
            {t('next.pane.openAddress')} <b>{address.text}</b>
          </span>
          <kbd className="hx-tl__kbd">↵</kbd>
        </button>
      )}
      <div id={listId} role="listbox" aria-label={t('next.pane.tools')}>
        {shown.map((g) => (
          <section key={g.id} className="hx-tl__group">
            <div className="hx-tl__label">{g.label}</div>
            <div className="hx-tl__grid">
              {g.items.map((x) => {
                const row = (
                  <button
                    id={rowId(x.id)}
                    type="button"
                    role="option"
                    tabIndex={-1}
                    className="hx-tl__tile"
                    data-i={Math.min(flat.indexOf(x), STAGGER_MAX)}
                    data-live={x.live ? 'true' : undefined}
                    aria-selected={current === x}
                    onClick={() => onOpen(x.id)}
                  >
                    <span className="hx-tl__ico">
                      <Icon name={x.icon} />
                    </span>
                    <span className="hx-tl__text">
                      <span className="hx-tl__name">{x.name}</span>
                      {x.live && <span className="hx-tl__about">{x.live}</span>}
                    </span>
                    {x.kbd && <kbd className="hx-tl__kbd">{x.kbd}</kbd>}
                  </button>
                );
                return x.hint ? (
                  <Tooltip key={x.id} label={x.hint} keys={x.kbd ? [x.kbd] : undefined}>
                    {row}
                  </Tooltip>
                ) : (
                  <span key={x.id} className="hx-tl__cell">
                    {row}
                  </span>
                );
              })}
            </div>
          </section>
        ))}
      </div>
      {found.length === 0 && !address && <div className="hx-tv__empty">{t('next.pane.noTool')}</div>}
    </div>
  );
};

export default ToolsLauncher;
