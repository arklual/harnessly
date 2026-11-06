// What the person does to the environment. Every change goes to the hub first; the screen shows what the hub kept, and
// the notice says what was done with «Отменить» when it can be taken back (which is another change to the hub).
import { useCallback, useState } from 'react';

import type {
  ApplyResponse,
  ApplyWhen,
  CatalogItem,
  CatalogKind,
  CatalogOwner,
  EnvEntryRow,
  EnvGroup,
  EnvLayer,
  EnvPreset,
  PatchGroupRequest,
  PatchPresetRequest,
} from '../../../../hub-api/env';
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

  /** A change of a group or of the place of an entry: the hub first, then the screen. */
  const change = async (
    fn: () => Promise<unknown>,
    text: string,
    undo?: () => Promise<unknown>,
  ): Promise<boolean> => {
    const ok = await run(async () => {
      try {
        await fn();
      } finally {
        await refresh();
      }
      return true;
    });
    if (!ok) return false;
    notice.say(
      text,
      undo &&
        (() =>
          void run(async () => {
            try {
              await undo();
            } finally {
              await refresh();
            }
          })),
    );
    return true;
  };

  /** «Применить на машинах»: an update on every machine the layer reaches, now or at its next start. */
  const applyMachines = async (t: Target, when: ApplyWhen): Promise<boolean> => {
    const out = await run(() => api.envApply(org, { layer: t.layer, scopeId: t.scopeId, when }));
    if (!out) return false;
    notice.say(applyText(out, when));
    return true;
  };

  const addGroup = (t: Target, name: string): Promise<boolean> =>
    change(
      () => api.envGroupAdd(org, { layer: t.layer, scopeId: t.scopeId, name }),
      `Группа «${name}» — по желанию: каждый включит её у себя сам`,
    );

  const patchGroup = (g: EnvGroup, req: PatchGroupRequest): Promise<boolean> => {
    const back: PatchGroupRequest = {};
    if (req.required !== undefined) back.required = g.required;
    if (req.defaultOn !== undefined) back.defaultOn = g.defaultOn;
    if (req.name !== undefined) back.name = g.name;
    let text = `Группа «${req.name ?? g.name}» сохранена`;
    if (req.required === true) text = `Группа «${g.name}» ставится всем`;
    if (req.required === false) text = `Группа «${g.name}» по желанию — каждый выбирает у себя`;
    if (req.defaultOn === true) text = `«${g.name}» сразу включена — кто не выбирал, получит её`;
    if (req.defaultOn === false) text = `«${g.name}» выключена, пока человек сам не включит`;
    return change(
      () => api.envGroupPatch(org, g.id, req),
      text,
      () => api.envGroupPatch(org, g.id, back),
    );
  };

  const removeGroup = (g: EnvGroup): Promise<boolean> =>
    change(
      () => api.envGroupRemove(org, g.id),
      `Группы «${g.name}» больше нет — её пункты остались в уровне`,
    );

  const moveToGroup = (row: EnvEntryRow, g: EnvGroup | null): Promise<boolean> =>
    change(
      () => api.envPatch(org, row.entryId, { groupId: g?.id ?? '' }),
      g ? `«${row.name}» теперь в группе «${g.name}»` : `«${row.name}» вне групп`,
      () => api.envPatch(org, row.entryId, { groupId: row.groupId }),
    );

  /** A member's own switch of an optional group. */
  const mineGroup = (groupId: string, name: string, on: boolean, space?: string): Promise<boolean> =>
    change(
      () => api.envMineGroup(groupId, on, space),
      on ? `«${name}» включена — машина поставит её` : `«${name}» выключена — новое из неё не поставится`,
      () => api.envMineGroup(groupId, !on, space),
    );

  const addPreset = (t: Target, name: string, groupIds: string[]): Promise<boolean> =>
    change(
      () => api.envPresetAdd(org, { layer: t.layer, scopeId: t.scopeId, name, groupIds }),
      `Пресет «${name}» готов — человек выберет его одним нажатием`,
    );

  const patchPreset = (p: EnvPreset, req: PatchPresetRequest): Promise<boolean> =>
    change(
      () => api.envPresetPatch(org, p.id, req),
      `Пресет «${req.name ?? p.name}» сохранён`,
      () => api.envPresetPatch(org, p.id, { name: p.name, hint: p.hint, groupIds: p.groupIds }),
    );

  const removePreset = (p: EnvPreset): Promise<boolean> =>
    change(() => api.envPresetRemove(org, p.id), `Пресета «${p.name}» больше нет — группы остались`);

  /** A member picks a preset: its groups go on, the other optional groups of the layer off. */
  const minePreset = (p: EnvPreset, space?: string): Promise<boolean> =>
    change(() => api.envMinePreset(p.id, space), `Выбран пресет «${p.name}» — машина обновит окружение`);

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

  return {
    busy,
    add,
    create,
    save,
    remove,
    pin,
    reorder,
    copy,
    run,
    applyMachines,
    addGroup,
    patchGroup,
    removeGroup,
    moveToGroup,
    mineGroup,
    addPreset,
    patchPreset,
    removePreset,
    minePreset,
  };
};

export type Actions = ReturnType<typeof useActions>;

const machines = (n: number) => `${n} ${plural(n, 'машине', 'машинах', 'машинах')}`;

/** What the toast says after «Применить на машинах». */
export const applyText = (out: ApplyResponse, when: ApplyWhen): string => {
  if (out.machines === 0) return 'Здесь пока нет машин — применится, когда они подключатся';
  if (when === 'next') return `Применится на ${machines(out.machines)} при следующем запуске`;
  if (out.waiting === 0) return `Применяем на ${machines(out.started)}`;
  if (out.started === 0)
    return `Машины не в сети — применится на ${machines(out.waiting)} при следующем запуске`;
  return `Применяем на ${machines(out.started)}, ещё на ${machines(out.waiting)} — при следующем запуске`;
};
