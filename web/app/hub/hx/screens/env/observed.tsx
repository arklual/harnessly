// «Уже используют на машинах»: the skills and MCP servers the chats of the screen called in the last 30 days, in the
// environment or not, and the explanation of the screen while it is empty. «Добавить» puts a tool into the layer the
// viewer adds to; a tool the catalog does not know yet first becomes an item of it (an organization admin's right), named
// so that its calls are counted for it.
import { useState } from 'react';

import type { EnvResponse, ObservedTool } from '../../../../hub-api/env';
import { OrgApiError, orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Btn, Icon } from '../../ui';
import { fmtNum, KIND_ICON, lastUsed, plural, type Target, withAdded } from './helpers';
import type { EnvView } from './state';

/** How the screen names a tool: a plugin's skill without the plugin, a plugin's or connector's MCP by its short name. */
export const toolLabel = (t: Pick<ObservedTool, 'kind' | 'name'>): { title: string; from: string } => {
  if (t.kind === 'skill') {
    const i = t.name.lastIndexOf(':');
    if (i < 0 || i === t.name.length - 1) return { title: t.name, from: '' };
    const plugin = t.name.slice(0, i);
    const skill = t.name.slice(i + 1);
    return { title: skill, from: plugin === skill ? '' : plugin };
  }
  const m = /^plugin_([^_]+)_(.+)$/.exec(t.name);
  if (m) return { title: m[2]!.replace(/_/g, ' '), from: m[1]! };
  if (t.name.startsWith('claude_ai_') && t.name.length > 10) {
    return { title: t.name.slice(10).replace(/_/g, ' '), from: 'claude.ai' };
  }
  return { title: t.name, from: '' };
};

const callsText = (n: number) => `${fmtNum(n)} ${plural(n, 'вызов', 'вызова', 'вызовов')}`;

const subOf = (t: ObservedTool, now: number): string => {
  const { from } = toolLabel(t);
  return [
    t.kind === 'skill' ? 'Скилл' : 'MCP',
    from ? `из ${from}` : '',
    `${callsText(t.calls)} за 30 дней`,
    t.callsWeek > 0 ? `${fmtNum(t.callsWeek)} за неделю` : '',
    t.people > 0 ? `${t.people} ${plural(t.people, 'человек', 'человека', 'человек')}` : '',
    `последний ${lastUsed(t.lastUsedAt, now)}`,
  ]
    .filter(Boolean)
    .join(' · ');
};

const key = (t: ObservedTool) => `${t.kind}:${t.name}`;

export const ObservedList = ({
  view,
  state,
  target,
  targetTitle,
  canCreate,
  now,
  onProblem,
}: {
  view: EnvResponse;
  state: EnvView;
  /** The layer «Добавить» puts a tool into; null: the viewer adds nothing here. */
  target: Target | null;
  targetTitle: string;
  /** The viewer may put a new item into the catalog (an organization admin). */
  canCreate: boolean;
  now: number;
  onProblem: (text: string) => void;
}) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());
  const [done, setDone] = useState<ReadonlySet<string>>(new Set());
  const list = view.observed;
  if (list.length === 0) return null;

  const add = async (t: ObservedTool) => {
    const k = key(t);
    if (!target || busy.has(k)) return;
    setBusy((s) => new Set(s).add(k));
    try {
      let itemId = t.itemId;
      if (!itemId) {
        const { title, from } = toolLabel(t);
        const item = await api.catalogAdd(org, {
          kind: t.kind,
          name: title,
          hint:
            t.kind === 'skill'
              ? `Уже стоит на машинах${from ? `, плагин ${from}` : ''}`
              : `Уже подключён на машинах${from ? `, ${from}` : ''}`,
          source: {},
        });
        itemId = item.id;
      }
      const id = itemId;
      const res = await state.write({
        apply: (v) => v,
        undo: (v) => v,
        run: () => api.envAdd(org, { layer: target.layer, scopeId: target.scopeId, itemId: id }),
        settle: (v, row) => withAdded(v, target, row),
      });
      if (res.ok || (res.error instanceof OrgApiError && res.error.code === 'exists')) {
        setDone((s) => new Set(s).add(k));
      } else {
        onProblem(orgErrorText(res.error));
      }
    } catch (e) {
      onProblem(orgErrorText(e));
    } finally {
      setBusy((s) => {
        const n = new Set(s);
        n.delete(k);
        return n;
      });
    }
  };
  // What is already in the environment shows its calls in the layers above; here only what is not there yet, the
  // tool just added stays until the next visit so the person sees it landed.
  const rows = list.filter((t) => !t.inEnv);
  const inside = list.length - rows.length;
  if (rows.length === 0) return null;
  const outside = rows.filter((t) => !done.has(key(t))).length;

  return (
    <section className="ad-card ob" aria-label="Уже используют на машинах">
      <h3>Уже используют на машинах, но не в окружении</h3>
      <p>
        {`Скиллы и MCP, которые чаты вызывали за 30 дней, а окружение их не даёт: ${outside}.`}{' '}
        {target
          ? `«Добавить» кладёт в слой «${targetTitle}» — новые люди получат это сами, а вызовы будут видны в слое.`
          : 'Добавить их может админ организации или лид команды.'}
        {inside > 0 ? ` Ещё ${inside} уже в окружении — их вызовы в слоях выше.` : ''}
      </p>
      {rows.map((t) => {
        const k = key(t);
        const { title } = toolLabel(t);
        const added = done.has(k);
        const may = target !== null && (t.itemId !== null || canCreate);
        return (
          <div className="ad-row" key={k}>
            <span className="ad-ic">
              <Icon i={KIND_ICON[t.kind]} size="sm" />
            </span>
            <span className="ad-row__t">
              <b>{title}</b>
              <small>{subOf(t, now)}</small>
            </span>
            {added ? (
              <span className="ad-add" data-on="1" aria-label="Добавлено в окружение">
                Добавлено
              </span>
            ) : may ? (
              <button
                type="button"
                className="ad-add"
                data-on="0"
                disabled={busy.has(k)}
                onClick={() => void add(t)}
              >
                {busy.has(k) ? 'Добавляем…' : 'Добавить'}
              </button>
            ) : (
              <span className="ad-row__v">не в окружении</span>
            )}
          </div>
        );
      })}
    </section>
  );
};

/** The screen with nothing in it: what the environment is and how it fills. */
export const EnvIntro = ({
  canAdd,
  hasObserved,
  known,
  onCatalog,
}: {
  canAdd: boolean;
  hasObserved: boolean;
  /** The hub has tool facts from the machines at all. */
  known: boolean;
  onCatalog: (() => void) | null;
}) => (
  <div className="ad-card ob-intro">
    <h3>Окружение — что получает каждый чат</h3>
    <p>
      Скиллы, MCP-серверы, репозитории и документы, которые хаб ставит на машины организации. Складывается из
      трёх слоёв: организация → тенант → команда. Человек видит у себя всё, что дали его слои, и может
      выключить незакреплённое.
    </p>
    <ol className="ob-steps">
      <li>
        <b>Возьмите то, чем уже пользуются.</b>{' '}
        {hasObserved
          ? 'Ниже — что чаты вызывали за месяц: «Добавить» кладёт это в окружение.'
          : known
            ? 'За месяц чаты не вызывали ни одного скилла и MCP.'
            : 'Список появится, когда машины пришлют первые отчёты о вызовах.'}
      </li>
      <li>
        <b>Или выберите из каталога</b> — там общие скиллы хаба и то, что завели админы организации.
      </li>
      <li>
        <b>Смотрите отдачу.</b> У каждой строки — вызовы за неделю и за 14 дней; то, что месяц никто не зовёт,
        помечается неиспользуемым.
      </li>
    </ol>
    {canAdd && onCatalog && (
      <Btn size="sm" icon="plus" onClick={onCatalog}>
        Открыть каталог
      </Btn>
    )}
  </div>
);
