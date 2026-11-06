// What the person does to the environment. Every change goes to the hub first; the screen shows what the hub kept, and
// the notice says what was done with «Отменить» when it can be taken back (which is another change to the hub).
import { useCallback, useState } from 'react';

import type { CatalogItem, CatalogKind, CatalogOwner, EnvEntryRow, EnvLayer } from '../../../../hub-api/env';
import { OrgApiError, orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { plural } from './format';
import { type Draft, hintOf, nameOf, type RepoRef, toSource } from './model';
import { chainOf, keyOf, metaOf, type Target } from './scope';

export interface Notice {
  say: (text: string, undo?: () => void) => void;
  fail: (text: string) => void;
}

/** The owner a new item gets: the layer being set up, none for the organization. */
export const ownerFor = (t: Target): CatalogOwner | undefined =>
  t.layer === 'org' ? undefined : { layer: t.layer, scopeId: t.scopeId };

/** Whether an item may be put into a layer: an item of a layer goes into that layer only. */
export const fitsLayer = (it: CatalogItem, t: Target): boolean =>
  it.owner === null || (it.owner.layer === t.layer && it.owner.scopeId === t.scopeId);

export const reposOf = (chain: readonly EnvLayer[]): RepoRef[] =>
  chain.flatMap((l) =>
    l.items.filter((r) => r.enabled && r.kind === 'repo').map((r) => ({ id: r.itemId, name: r.name })),
  );

export const useActions = (
  layers: readonly EnvLayer[],
  refresh: () => Promise<void>,
  refreshCatalog: () => Promise<void>,
  notice: Notice,
) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const [busy, setBusy] = useState(false);

  const sync = useCallback(async () => {
    await Promise.all([refresh(), refreshCatalog()]);
  }, [refresh, refreshCatalog]);

  /** Runs a change: the busy flag around it, the failure told. */
  const run = useCallback(
    async <T>(fn: () => Promise<T>): Promise<T | null> => {
      setBusy(true);
      try {
        return await fn();
      } catch (e) {
        notice.fail(orgErrorText(e));
        return null;
      } finally {
        setBusy(false);
      }
    },
    [notice],
  );

  const metaFor = (t: Target) => {
    const l = layers.find((x) => x.layer === t.layer && x.scopeId === t.scopeId);
    return l ? metaOf(l) : null;
  };

  const undoAdd = (entryIds: string[]) => () =>
    void run(async () => {
      for (const id of entryIds) await api.envRemove(org, id);
      await sync();
    });

  const add = async (
    t: Target,
    items: readonly CatalogItem[],
    pinned: boolean | ((it: CatalogItem) => boolean),
  ): Promise<string[] | null> => {
    const ids = await run(async () => {
      const made: string[] = [];
      try {
        for (const it of items) {
          const row = await api.envAdd(org, {
            layer: t.layer,
            scopeId: t.scopeId,
            itemId: it.id,
            pinned: typeof pinned === 'function' ? pinned(it) : pinned,
          });
          made.push(row.entryId);
        }
      } finally {
        await sync();
      }
      return made;
    });
    if (ids && ids.length) {
      const where = metaFor(t)?.acc ?? '';
      const what =
        items.length === 1
          ? `«${items[0]?.name ?? ''}»`
          : `${items.length} ${plural(items.length, 'пункт', 'пункта', 'пунктов')}`;
      notice.say(`Добавили ${what} в ${where} — поставится каждому само`, undoAdd(ids));
    }
    return ids;
  };

  const create = async (
    t: Target,
    d: Draft,
    pinned: boolean,
    repos: readonly RepoRef[],
  ): Promise<string | null> => {
    const res = await run(async () => {
      const item = await api.catalogAdd(org, {
        kind: d.k,
        name: nameOf(d),
        hint: hintOf(d, repos),
        source: toSource(d),
        owner: ownerFor(t),
      });
      try {
        return await api.envAdd(org, { layer: t.layer, scopeId: t.scopeId, itemId: item.id, pinned });
      } finally {
        await sync();
      }
    });
    if (!res) return null;
    notice.say(
      `Добавили «${nameOf(d)}» в ${metaFor(t)?.acc ?? ''} — поставится каждому само`,
      undoAdd([res.entryId]),
    );
    return res.entryId;
  };

  const save = async (
    row: EnvEntryRow,
    item: CatalogItem,
    d: Draft,
    pinned: boolean,
    repos: readonly RepoRef[],
  ): Promise<boolean> => {
    const next = { name: nameOf(d), hint: hintOf(d, repos), source: toSource(d) };
    const changed =
      JSON.stringify(next) !== JSON.stringify({ name: item.name, hint: item.hint, source: item.source });
    const ok = await run(async () => {
      try {
        if (changed) await api.catalogPatch(org, item.id, next);
        if (pinned !== row.pinned) await api.envPatch(org, row.entryId, { pinned });
      } finally {
        await sync();
      }
      return true;
    });
    if (!ok) return false;
    notice.say(
      `Сохранили «${next.name}» — машины подтянут сами`,
      () =>
        void run(async () => {
          try {
            if (changed)
              await api.catalogPatch(org, item.id, { name: item.name, hint: item.hint, source: item.source });
            if (pinned !== row.pinned) await api.envPatch(org, row.entryId, { pinned: row.pinned });
          } finally {
            await sync();
          }
        }),
    );
    return true;
  };

  const remove = async (t: Target, row: EnvEntryRow): Promise<boolean> => {
    const ok = await run(async () => {
      try {
        await api.envRemove(org, row.entryId);
      } finally {
        await sync();
      }
      return true;
    });
    if (!ok) return false;
    const keeps =
      row.kind === 'repo' || row.kind === 'access'
        ? ' Уже склонированное и поставленное у людей останется.'
        : ' Уйдёт из чатов при следующей проверке машин.';
    notice.say(
      `Убрали «${row.name}» из ${metaFor(t)?.gen ?? ''}.${keeps}`,
      () =>
        void run(async () => {
          try {
            await api.envAdd(org, {
              layer: t.layer,
              scopeId: t.scopeId,
              itemId: row.itemId,
              pinned: row.pinned,
            });
          } finally {
            await sync();
          }
        }),
    );
    return true;
  };

  const pin = async (row: EnvEntryRow): Promise<void> => {
    const on = !row.pinned;
    const ok = await run(async () => {
      try {
        await api.envPatch(org, row.entryId, { pinned: on });
      } finally {
        await refresh();
      }
      return true;
    });
    if (!ok) return;
    notice.say(
      on
        ? `«${row.name}» теперь обязательно — поставится всем`
        : `«${row.name}» по желанию — человек сможет пропустить`,
      () =>
        void run(async () => {
          try {
            await api.envPatch(org, row.entryId, { pinned: row.pinned });
          } finally {
            await refresh();
          }
        }),
    );
  };

  const reorder = async (t: Target, entryIds: string[], text?: string): Promise<boolean> => {
    const layer = layers.find((l) => keyOf(l) === keyOf(t));
    const before = layer?.items.filter((r) => r.kind === 'script').map((r) => r.entryId) ?? [];
    const kept = await run(async () => {
      try {
        return await api.envOrder(org, { layer: t.layer, scopeId: t.scopeId, entryIds });
      } finally {
        await refresh();
      }
    });
    if (!kept) return false;
    const names = new Map(layer?.items.map((r) => [r.entryId, r.name]));
    const moved = entryIds.find((id, i) => before[i] !== id && before.indexOf(id) !== i) ?? entryIds[0] ?? '';
    notice.say(
      text ?? `Порядок изменился: «${names.get(moved) ?? ''}» теперь на новом месте`,
      () =>
        void run(async () => {
          try {
            await api.envOrder(org, { layer: t.layer, scopeId: t.scopeId, entryIds: before });
          } finally {
            await refresh();
          }
        }),
    );
    return true;
  };

  /**
   * Takes items of another team into this one. An item of the organization is put in as it is; an item that belongs to the
   * other team is copied (an item of a layer goes into that layer only), and a script is pointed at the copy of its
   * repository. The repository a copied script needs comes along.
   */
  const copy = async (
    src: EnvLayer,
    t: Target,
    itemIds: readonly string[],
    catalog: readonly CatalogItem[],
  ): Promise<boolean> => {
    const have = new Set(chainOf(layers, t).flatMap((l) => l.items.map((r) => r.itemId)));
    const byId = new Map(catalog.map((c) => [c.id, c]));
    const picked = new Set(itemIds);
    for (const id of itemIds) {
      const need = byId.get(id)?.source.repo;
      if (need && !have.has(need) && src.items.some((r) => r.itemId === need)) picked.add(need);
    }
    const order: CatalogKind[] = ['access', 'repo', 'script', 'skill', 'mcp', 'doc'];
    const list = [...picked]
      .map((id) => byId.get(id))
      .filter((c): c is CatalogItem => !!c && !have.has(c.id))
      .sort((a, b) => order.indexOf(a.kind) - order.indexOf(b.kind));
    const suffix = metaFor(t)?.short ?? '';
    const made = await run(async () => {
      const ids: string[] = [];
      const remap = new Map<string, string>();
      try {
        for (const it of list) {
          const pinned = src.items.find((r) => r.itemId === it.id)?.pinned ?? false;
          let itemId = it.id;
          if (it.owner !== null) {
            const source = { ...it.source };
            if (source.repo && remap.has(source.repo)) source.repo = remap.get(source.repo);
            const body = { kind: it.kind, hint: it.hint, source, owner: ownerFor(t) };
            let copied: CatalogItem;
            try {
              copied = await api.catalogAdd(org, { ...body, name: it.name });
            } catch (e) {
              if (!(e instanceof OrgApiError) || e.code !== 'name_taken') throw e;
              copied = await api.catalogAdd(org, { ...body, name: `${it.name} (${suffix})` });
            }
            remap.set(it.id, copied.id);
            itemId = copied.id;
          }
          const row = await api.envAdd(org, { layer: t.layer, scopeId: t.scopeId, itemId, pinned });
          ids.push(row.entryId);
        }
      } finally {
        await sync();
      }
      return ids;
    });
    if (!made) return false;
    if (made.length > 0) {
      const what =
        made.length === 1
          ? `«${list[0]?.name ?? ''}»`
          : `${made.length} ${plural(made.length, 'пункт', 'пункта', 'пунктов')}`;
      notice.say(`Скопировали ${what} из ${metaOf(src).gen} — поставится каждому само`, undoAdd(made));
    }
    return true;
  };

  return { busy, add, create, save, remove, pin, reorder, copy, run };
};

export type Actions = ReturnType<typeof useActions>;
