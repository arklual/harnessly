// The tabs of «Окружение» as the board draws them: «Что стоит» (one flat table of everything the layers give),
// «Каталог» (cards of what can be added) and «Применение» (how far the machines got, logs and the sandbox check).
import type {
  CatalogItem,
  CatalogKind,
  EnvDelivery,
  EnvEntryRow,
  EnvLayer,
  EnvMachineDelivery,
  ItemDelivery,
} from '../../../../hub-api/env';
import { Btn, Icon } from '../../ui';
import { KIND } from './model';
import { metaOf } from './scope';

export type TabId = 'items' | 'catalog' | 'apply';

export const TabNav = ({ tab, onTab }: { tab: TabId; onTab: (t: TabId) => void }) => (
  <nav className="hx-set__nav hb-tabs hd-tabs">
    {(
      [
        ['items', 'Что стоит'],
        ['catalog', 'Каталог'],
        ['apply', 'Применение'],
      ] as [TabId, string][]
    ).map(([id, label]) => (
      <button
        key={id}
        type="button"
        className="hx-set__nav-item"
        aria-current={tab === id}
        onClick={() => onTab(id)}
      >
        <span>{label}</span>
      </button>
    ))}
  </nav>
);

const kindWord = (k: CatalogKind) => KIND[k]?.one ?? k;

const stateOf = (r: EnvEntryRow): { cls: string; text: string } | null => {
  if (!r.enabled || r.off) return { cls: 'warn', text: 'выключено' };
  const d = r.delivery;
  if (!d) return null;
  if (d.error > 0) return { cls: 'bad', text: `ошибки на ${d.error}` };
  if (d.pending > 0) return { cls: 'warn', text: `ждёт на ${d.pending}` };
  return { cls: 'ok', text: 'работает' };
};

export const ItemsTable = ({
  chain,
  canRemove,
  onRemove,
}: {
  chain: EnvLayer[];
  canRemove: (layer: EnvLayer, row: EnvEntryRow) => boolean;
  onRemove: (layer: EnvLayer, row: EnvEntryRow) => void;
}) => {
  const rows = chain.flatMap((layer) => layer.items.map((row) => ({ layer, row })));
  const hasState = rows.some(({ row }) => stateOf(row) !== null);
  return (
    <div className="t-card viz-card dash-tile hd-card flat">
      <table className="tbl">
        <thead>
          <tr>
            <th>Вид</th>
            <th>Что</th>
            <th>Слой</th>
            {hasState && <th>Состояние</th>}
            <th />
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={hasState ? 5 : 4} className="mut2">
                В окружении пока ничего нет
              </td>
            </tr>
          )}
          {rows.map(({ layer, row }) => {
            const m = metaOf(layer);
            const st = stateOf(row);
            return (
              <tr key={row.entryId}>
                <td>
                  <span className="pill">{kindWord(row.kind)}</span>
                </td>
                <td>
                  <div className="nm">{row.name}</div>
                  {row.hint && <div className="mut2 sm">{row.hint}</div>}
                </td>
                <td>
                  <span className={`pill ${m.kind === 'org' ? 'acc' : 'info'}`}>{layer.title}</span>
                </td>
                {hasState && (
                  <td>
                    {st ? (
                      <span className={`pill ${st.cls}`}>{st.text}</span>
                    ) : (
                      <span className="mut2">—</span>
                    )}
                  </td>
                )}
                <td className="num">
                  {canRemove(layer, row) ? (
                    <Btn variant="ghost" size="sm" onClick={() => onRemove(layer, row)}>
                      Убрать
                    </Btn>
                  ) : (
                    <span className="mut2 sm">от слоя выше</span>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

export const CatalogCards = ({
  items,
  kinds,
  canAdd,
  onAdd,
}: {
  items: CatalogItem[] | null;
  /** The kinds the add sheet can start from; shown when the catalog is not readable. */
  kinds: CatalogKind[];
  canAdd: boolean;
  onAdd: (kind: CatalogKind) => void;
}) => {
  const list = (items ?? []).filter((c) => !c.archived);
  const cards = list.length
    ? list.map((c) => ({ key: c.id, kind: c.kind, name: c.name, hint: c.hint, added: c.added }))
    : kinds.map((k) => ({ key: k, kind: k, name: KIND[k].one, hint: KIND[k].about, added: false }));
  return (
    <div className="three">
      {cards.map((c) => (
        <div className="t-card viz-card dash-tile hd-card" key={c.key}>
          <span className="pill">{kindWord(c.kind)}</span>
          <h3 style={{ marginTop: 8 }}>{c.name}</h3>
          <div className="hint">{c.hint}</div>
          {c.added ? (
            <span className="pill ok">уже добавлено</span>
          ) : (
            canAdd && (
              <Btn size="sm" icon="plus" onClick={() => onAdd(c.kind)}>
                Добавить
              </Btn>
            )
          )}
        </div>
      ))}
    </div>
  );
};

const sum = (d: ItemDelivery | null) => (d ? d.ok + d.pending + d.error : 0);

// How far one machine got: off line, never reported, an older list, or ok of total items of the current one in.
const MachinePill = ({ m }: { m: EnvMachineDelivery }) => {
  if (!m.online && m.state !== 'current') return <span className="pill bad">машина не на связи</span>;
  if (m.state === 'silent') return <span className="pill warn">ещё не ответила</span>;
  if (m.state === 'stale') return <span className="pill warn">старый список · ждёт</span>;
  if (m.total > 0 && m.ok < m.total)
    return (
      <span className="pill warn">
        {m.ok} из {m.total} · ждёт
      </span>
    );
  return <span className="pill ok">{m.total > 0 ? `${m.ok} из ${m.total}` : 'актуально'}</span>;
};

export const ApplyPanel = ({
  delivery,
  rows,
  onLogs,
  onTest,
}: {
  delivery: EnvDelivery | null;
  rows: EnvEntryRow[];
  onLogs: () => void;
  onTest: (() => void) | null;
}) => {
  const withD = rows.filter((r) => r.delivery && sum(r.delivery) > 0);
  const perMachine = delivery?.rows && delivery.rows.length > 0 ? delivery.rows : null;
  const tools = (
    <>
      <Btn variant="ghost" size="sm" onClick={onLogs}>
        <Icon i="file-text" size="sm" />
        Логи
      </Btn>
      {onTest && (
        <Btn variant="ghost" size="sm" onClick={onTest}>
          <Icon i="shield-check" size="sm" />
          Проверить
        </Btn>
      )}
    </>
  );
  return (
    <>
      {perMachine ? (
        <div className="t-card viz-card dash-tile hd-card flat">
          <table className="tbl">
            <thead>
              <tr>
                <th>Машина</th>
                <th>Применено</th>
                <th>Последний раз</th>
                <th className="num">{tools}</th>
              </tr>
            </thead>
            <tbody>
              {perMachine.map((m) => (
                <tr key={m.id}>
                  <td className="nm">{m.name}</td>
                  <td>
                    <MachinePill m={m} />
                  </td>
                  <td className="mut">{m.at ? new Date(m.at).toLocaleString('ru-RU') : '—'}</td>
                  <td />
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="t-card viz-card dash-tile hd-card flat">
          <table className="tbl">
            <thead>
              <tr>
                <th>Машины</th>
                <th>Применено</th>
                <th>Последний раз</th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr>
                <td className="nm">{delivery ? `${delivery.machines} в работе` : 'Нет данных'}</td>
                <td>
                  {delivery ? (
                    <span className={`pill ${delivery.current >= delivery.machines ? 'ok' : 'warn'}`}>
                      {delivery.current} из {delivery.machines} со свежим списком
                    </span>
                  ) : (
                    <span className="mut2">—</span>
                  )}
                  {delivery && delivery.reported < delivery.machines && (
                    <span className="mut2 sm"> · ответили {delivery.reported}</span>
                  )}
                </td>
                <td className="mut">
                  {delivery?.lastAt ? new Date(delivery.lastAt).toLocaleString('ru-RU') : '—'}
                </td>
                <td className="num">
                  <Btn variant="ghost" size="sm" onClick={onLogs}>
                    <Icon i="file-text" size="sm" />
                    Логи
                  </Btn>
                  {onTest && (
                    <Btn variant="ghost" size="sm" onClick={onTest}>
                      <Icon i="shield-check" size="sm" />
                      Проверить
                    </Btn>
                  )}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      )}
      {withD.length > 0 && (
        <div className="t-card viz-card dash-tile hd-card flat" style={{ marginTop: 16 }}>
          <table className="tbl">
            <thead>
              <tr>
                <th>Пункт</th>
                <th>Применено</th>
                <th>Что не так</th>
              </tr>
            </thead>
            <tbody>
              {withD.map((r) => {
                const d = r.delivery as ItemDelivery;
                return (
                  <tr key={r.entryId}>
                    <td className="nm">{r.name}</td>
                    <td>
                      <span className={`pill ${d.error ? 'bad' : d.pending ? 'warn' : 'ok'}`}>
                        {d.ok} из {sum(d)}
                      </span>
                    </td>
                    <td className="mut">{d.message ?? '—'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
};
