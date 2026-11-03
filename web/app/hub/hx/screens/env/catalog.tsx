// The catalog drawer: skills, MCP, repositories and documents the viewer may put into a layer. «Добавить» puts the item in
// at once and takes it back, saying why, when the hub refuses; «Добавлено» takes it out again.
import { useEffect, useState } from 'react';

import type { CatalogItem, EnvResponse } from '../../../../hub-api/env';
import { OrgApiError, orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Drawer, Icon } from '../../ui';
import { entryIn, KIND_ICON, KIND_WORD, type Target, withAdded, withoutEntry } from './helpers';
import type { EnvView } from './state';

export const CatalogDrawer = ({
  open,
  target,
  targetTitle,
  view,
  state,
  onClose,
  onProblem,
}: {
  open: boolean;
  target: Target | null;
  /** The name of the layer the items go into, for the small line under the title. */
  targetTitle: string;
  view: EnvResponse;
  state: EnvView;
  onClose: () => void;
  onProblem: (text: string) => void;
}) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const [tick, setTick] = useState(0);
  const layer = target?.layer;
  const scopeId = target?.scopeId;

  useEffect(() => {
    if (!open || !layer || !scopeId) return;
    let alive = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setItems(null);
    setError(null);
    api.envCatalog(org, { layer, scopeId }).then(
      (list) => alive && setItems(list),
      (e: unknown) => alive && setError(e),
    );
    return () => {
      alive = false;
    };
  }, [api, org, open, layer, scopeId, tick]);

  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setQ('');
  }

  const mark = (id: string, on: boolean) =>
    setItems((l) => (l ? l.map((i) => (i.id === id ? { ...i, added: on } : i)) : l));
  const hold = (id: string, on: boolean) =>
    setBusy((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });

  const toggle = async (item: CatalogItem) => {
    if (!target || busy.has(item.id)) return;
    hold(item.id, true);
    const want = !item.added;
    mark(item.id, want);
    if (want) {
      const res = await state.write({
        apply: (v) => v,
        undo: (v) => v,
        run: () => api.envAdd(org, { layer: target.layer, scopeId: target.scopeId, itemId: item.id }),
        settle: (v, row) => withAdded(v, target, row),
      });
      if (!res.ok) {
        // «already there» means the screen was stale: it is there, so leave it marked.
        const exists = res.error instanceof OrgApiError && res.error.code === 'exists';
        if (!exists) mark(item.id, false);
        onProblem(orgErrorText(res.error));
      }
    } else {
      const row = entryIn(view, target, item.id);
      if (!row) {
        mark(item.id, false);
      } else {
        const res = await state.write({
          apply: (v) => withoutEntry(v, row.entryId),
          undo: (v) => withAdded(v, target, row),
          run: () => api.envRemove(org, row.entryId),
        });
        if (!res.ok) {
          mark(item.id, true);
          onProblem(orgErrorText(res.error));
        }
      }
    }
    hold(item.id, false);
  };

  const needle = q.trim().toLowerCase();
  const shown = (items ?? []).filter(
    (i) => !needle || [i.name, i.hint, KIND_WORD[i.kind]].some((x) => x.toLowerCase().includes(needle)),
  );

  return (
    <Drawer
      open={open}
      onClose={onClose}
      className="cat"
      label={targetTitle ? `Каталог, добавляем в: ${targetTitle}` : 'Каталог'}
      title="Каталог"
    >
      <label className="ad-search">
        <Icon i="search" size="sm" />
        <input
          type="search"
          placeholder="Скиллы, MCP, репозитории, доки"
          aria-label="Найти в каталоге"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape' && q) {
              e.stopPropagation();
              setQ('');
            }
          }}
        />
      </label>
      {error != null ? (
        <div className="env-fail" role="alert" style={{ margin: '8px 0' }}>
          <Icon i="circle-alert" />
          <span>{orgErrorText(error)}</span>
          <button type="button" className="hx-btn" data-size="sm" onClick={() => setTick((n) => n + 1)}>
            Повторить
          </button>
        </div>
      ) : items === null ? (
        <div className="env-sk__rows" aria-busy="true" aria-label="Загружаем">
          {[0, 1, 2, 3, 4].map((i) => (
            <span key={i} className="hx-sk" style={{ height: 52 }} />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <div className="env-empty">{needle ? 'Ничего не нашлось' : 'В каталоге пока пусто'}</div>
      ) : (
        shown.map((i) => (
          <div className="ad-row" key={i.id}>
            <span className="ad-ic">
              <Icon i={KIND_ICON[i.kind]} size="sm" />
            </span>
            <span className="ad-row__t">
              <b>{i.name}</b>
              <small>
                {KIND_WORD[i.kind]} · {i.hint}
              </small>
            </span>
            <button
              type="button"
              className="ad-add"
              data-on={i.added ? '1' : '0'}
              disabled={busy.has(i.id)}
              aria-pressed={i.added ? 'true' : 'false'}
              onClick={() => void toggle(i)}
            >
              {i.added ? 'Добавлено' : 'Добавить'}
            </button>
          </div>
        ))
      )}
    </Drawer>
  );
};
