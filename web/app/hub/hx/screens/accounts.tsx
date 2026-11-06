// «Аккаунты»: the subscriptions and API keys the organization's agents work on — who pays, who used each
// this month and what it cost at list prices. An account the hub only found on a machine waits on top until an
// administrator says what it is; the card and its form live in the address (`?a=<id>`, `&dlg=register`).
import { type CSSProperties, type FormEvent, useState } from 'react';

import { navigate } from '../../lib/router';
import type { Account, AccountFunding, AccountKind, RegisterAccount } from '../../org/api/accounts';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { useLoad } from '../../org/hooks';
import { ListBar, NoMatch, useListView, type SortKey } from '../sort-bar';
import { searchRows, sortRows } from '../sort';
import { Titles } from '../shell';
import { Btn, Drawer, Icon, initial } from '../ui';
import { inRubles, shownOfUsd } from '../../org/money';
import { fromUsdExact } from '../../org/screens/overview/format';
import { plural, rub, usd } from './overview/fmt';
import { useUrl } from './teams/url';
import '../styles/accounts.css';

const CARDS = ['a', 'dlg'] as const;

const PROVIDER: Record<string, string> = { anthropic: 'Anthropic', openai: 'OpenAI' };
const KIND: Record<AccountKind, string> = {
  subscription: 'подписка',
  api_key: 'ключ API',
  prepaid: 'предоплата',
};
const FUNDING: Record<AccountFunding, string> = { org: 'Организация', personal: 'Сам человек' };

/** The common plans: a click fills the plan and the fee. */
const PLANS: { provider: string; plan: string; label: string; fee: number }[] = [
  { provider: 'anthropic', plan: 'pro', label: 'Claude Pro · $20', fee: 20 },
  { provider: 'anthropic', plan: 'max5', label: 'Max 5x · $100', fee: 100 },
  { provider: 'anthropic', plan: 'max', label: 'Max 20x · $200', fee: 200 },
  { provider: 'openai', plan: 'plus', label: 'ChatGPT Plus · $20', fee: 20 },
  { provider: 'openai', plan: 'pro', label: 'ChatGPT Pro · $200', fee: 200 },
];

/** A dollar price with what it comes to in the shown money, «$100 ≈ 9 200 ₽»; just «$100» while there is no rate. */
const usdAndShown = (n: number): string =>
  inRubles() && shownOfUsd(n) !== null ? `${usd(n)} ≈ ${fromUsdExact(n)}` : usd(n);

const providerName = (p: string): string => PROVIDER[p] ?? p;

const planName = (a: Pick<Account, 'provider' | 'plan'>): string =>
  PLANS.find((x) => x.provider === a.provider && x.plan === a.plan)?.label.split(' · ')[0] ?? a.plan;

const subOf = (a: Account, tenantName: (id: string | null) => string): string =>
  [providerName(a.provider), planName(a) || null, KIND[a.kind], tenantName(a.tenantId)]
    .filter(Boolean)
    .join(' · ');

/** A found account has only the hub's technical label: name it by the provider and whom it was seen with. */
const titleOf = (a: Account): string => {
  if (!a.discovered) return a.label;
  const who = a.owner?.name ?? a.people[0]?.name;
  return `${providerName(a.provider)}${who ? ` у ${who}` : ''}: не зарегистрирован`;
};

const peopleLine = (a: Account): string => {
  if (a.people.length === 0) return 'никто в этом месяце';
  const names = a.people.slice(0, 2).map((p) => p.name);
  const more = a.people.length - names.length;
  return more > 0 ? `${names.join(', ')} и ещё ${more}` : names.join(', ');
};

const Seg = <T,>({
  items,
  value,
  label,
  onPick,
  wrap,
}: {
  items: { id: T; label: string }[];
  value: T;
  label: string;
  onPick: (v: T) => void;
  wrap?: boolean;
}) => (
  <div className={wrap ? 'ac-chips' : 'hx-set__seg'} role="radiogroup" aria-label={label}>
    {items.map((o) => (
      <button
        key={String(o.id)}
        type="button"
        className={wrap ? 'ac-chip' : 'hx-set__seg-item'}
        role="radio"
        aria-checked={o.id === value}
        onClick={() => onPick(o.id)}
      >
        {o.label}
      </button>
    ))}
  </div>
);

// ── The list ────────────────────────────────────────────────────────────────────────────────────────────────────────

const Row = ({ a, sub, onOpen }: { a: Account; sub: string; onOpen: () => void }) => (
  <button
    type="button"
    className="pt-row ac-row"
    data-found={a.discovered ? '1' : undefined}
    onClick={onOpen}
  >
    <span className="pt-name">
      <span className="ad-ic">
        <Icon i={a.discovered ? 'circle-alert' : a.kind === 'api_key' ? 'key-round' : 'coins'} size="sm" />
      </span>
      <span className="mt-name__t">
        <b title={a.discovered ? a.label : undefined}>{titleOf(a)}</b>
        <small>{sub}</small>
      </span>
    </span>
    <span className="ac-pay">{a.discovered ? 'не указано' : FUNDING[a.funding]}</span>
    <span className="ac-who">{peopleLine(a)}</span>
    <span
      className="ac-num"
      title={a.kind === 'subscription' && a.feeUsd > 0 ? `${usd(a.feeUsd)} в месяц` : undefined}
    >
      {a.kind === 'subscription' && a.feeUsd > 0 ? `${fromUsdExact(a.feeUsd)}/мес` : '—'}
    </span>
    <span className="ac-num">
      <b>{rub(a.monthRub)}</b>
    </span>
    <span className="ac-go">
      {a.discovered ? <span className="ac-cta">Зарегистрировать</span> : <Icon i="chevron-right" size="sm" />}
    </span>
  </button>
);

const Head = () => (
  <div className="pt-row ac-row mt-head">
    <span>Аккаунт</span>
    <span>Кто платит</span>
    <span>Кто пользуется</span>
    <span>Абонентская</span>
    <span>В месяце по прайсу</span>
    <span />
  </div>
);

// ── The card ────────────────────────────────────────────────────────────────────────────────────────────────────────

const Card = ({
  a,
  tenant,
  canManage,
  base,
  onEdit,
}: {
  a: Account;
  tenant: string;
  canManage: boolean;
  base: string;
  onEdit: () => void;
}) => (
  <>
    <div className="md-res ps-stats">
      <div>
        <b>{rub(a.monthRub)}</b>
        <small>в этом месяце по прайсу API</small>
      </div>
      <div>
        <b>{a.kind === 'subscription' && a.feeUsd > 0 ? fromUsdExact(a.feeUsd) : '—'}</b>
        <small>
          {a.kind === 'subscription' && a.feeUsd > 0
            ? `абонентская плата в месяц · ${usd(a.feeUsd)}`
            : 'абонентская плата в месяц'}
        </small>
      </div>
      <div>
        <b>{a.people.length}</b>
        <small>{plural(a.people.length, 'человек', 'человека', 'человек')} пользуются</small>
      </div>
    </div>
    <div className="md-sec">
      <div className="md-sec__t">Кто платит</div>
      <p className="ac-line">
        {FUNDING[a.funding]}
        {a.owner ? ` · владелец ${a.owner.name}` : ''}
      </p>
      <p className="md-note">
        {a.funding === 'org'
          ? 'Расход идёт в бюджет и лимиты команды.'
          : 'Человек платит сам: расход виден, но в бюджет организации не входит.'}{' '}
        {`Доступен: ${tenant.toLowerCase()}.`}
      </p>
    </div>
    <div className="md-sec">
      <div className="md-sec__t">Кто пользуется в этом месяце</div>
      {a.people.length === 0 ? (
        <p className="md-note">Пока никто.</p>
      ) : (
        <ul className="ac-people">
          {a.people.map((p) => (
            <li key={p.userId}>
              <button
                type="button"
                onClick={() => navigate(`${base}/teams?p=${encodeURIComponent(p.userId)}`)}
              >
                <span className="hx-avatar ac-av">{initial(p.name)}</span>
                {p.name}
                <Icon i="arrow-up-right" size="xs" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
    {!canManage && (
      <div className="md-sec">
        <p className="md-note">Менять аккаунты может администратор организации.</p>
      </div>
    )}
    {canManage && (
      <div className="md-sec">
        <Btn variant="outline" size="sm" icon="pencil" onClick={onEdit}>
          Изменить
        </Btn>
      </div>
    )}
  </>
);

// ── The form ────────────────────────────────────────────────────────────────────────────────────────────────────────

const Form = ({
  a,
  tenants,
  onDone,
  onCancel,
}: {
  a: Account;
  tenants: { id: string; name: string }[];
  onDone: (saved: Account | null) => void;
  onCancel: () => void;
}) => {
  const { api } = useOrg();
  const plans = PLANS.filter((p) => p.provider === a.provider);
  const [label, setLabel] = useState(a.discovered ? '' : a.label);
  const [plan, setPlan] = useState(a.plan);
  const [kind, setKind] = useState<AccountKind>(a.kind);
  const [funding, setFunding] = useState<AccountFunding>(a.discovered ? 'org' : a.funding);
  const [fee, setFee] = useState(a.feeUsd > 0 ? String(a.feeUsd) : '');
  const sole = a.people.length === 1 ? a.people[0]!.userId : null;
  const [owner, setOwner] = useState<string | null>(a.owner?.userId ?? sole);
  const [tenant, setTenant] = useState<string | null>(a.tenantId);
  const [history, setHistory] = useState(a.discovered);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const clean = label.trim();
  const feeNum = fee.trim() === '' ? null : Number(fee.replace(',', '.'));
  const feeBad =
    kind === 'subscription' && feeNum !== null && (!Number.isFinite(feeNum) || feeNum < 0 || feeNum > 100000);
  const bad = clean === '' || clean.length > 80 || feeBad;
  const ownerName = a.people.find((p) => p.userId === owner)?.name ?? a.owner?.name ?? '';

  const pickPlan = (p: (typeof PLANS)[number]) => {
    setPlan(p.plan);
    setKind('subscription');
    setFee(String(p.fee));
    if (!clean) setLabel(`${p.label.split(' · ')[0]}${ownerName ? ` · ${ownerName}` : ''}`);
  };

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (bad || busy) return;
    setBusy(true);
    setProblem(null);
    const body: RegisterAccount = {
      label: clean,
      plan: plan.trim(),
      kind,
      funding,
      monthlyFeeUsd: kind === 'subscription' ? feeNum : null,
      ownerUserId: owner,
      tenantId: tenant,
      history: history && owner !== null,
    };
    try {
      const saved = await api.registerAccount(a.id, body);
      onDone(saved);
    } catch (err) {
      setProblem(orgErrorText(err));
    } finally {
      setBusy(false);
    }
  };

  const ownerItems = [
    ...a.people.map((p) => ({ id: p.userId as string | null, label: p.name })),
    ...(a.owner && !a.people.some((p) => p.userId === a.owner!.userId)
      ? [{ id: a.owner.userId as string | null, label: a.owner.name }]
      : []),
    { id: null as string | null, label: 'Не указан' },
  ];

  return (
    <form onSubmit={(e) => void submit(e)}>
      {a.discovered && (
        <div className="md-sec">
          <p className="ac-found">
            <Icon i="circle-alert" size="sm" />
            <span>
              {`Хаб нашёл этот аккаунт ${providerName(a.provider)} на машине${a.people.length ? ` (${peopleLine(a)})` : ''}, но в списке организации его нет. Пока он не зарегистрирован, его расход считается личным и не входит в лимиты.`}
            </span>
          </p>
        </div>
      )}
      {plans.length > 0 && (
        <div className="md-sec">
          <div className="md-sec__t">Тариф</div>
          <div className="ac-chips">
            {plans.map((p) => (
              <button
                key={p.plan}
                type="button"
                className="ac-chip"
                aria-pressed={plan === p.plan && kind === 'subscription'}
                onClick={() => pickPlan(p)}
              >
                {p.label.split(' · ')[0]} · {usdAndShown(p.fee)}
              </button>
            ))}
          </div>
        </div>
      )}
      <div className="md-sec">
        <div className="md-sec__t">Название</div>
        <input
          className="hx-set__input"
          value={label}
          maxLength={80}
          autoFocus
          placeholder="Например, Claude Max · Глеб"
          aria-label="Название аккаунта"
          onChange={(e) => setLabel(e.target.value)}
        />
        <p className="md-note">Так аккаунт будет называться в расходах и сигналах.</p>
      </div>
      <div className="md-sec">
        <div className="md-sec__t">Вид</div>
        <Seg
          label="Вид аккаунта"
          items={(Object.keys(KIND) as AccountKind[]).map((k) => ({ id: k, label: KIND[k] }))}
          value={kind}
          onPick={setKind}
        />
        {kind === 'subscription' && (
          <label className="ac-fee">
            <span>Абонентская плата</span>
            <input
              className="hx-set__input"
              inputMode="decimal"
              value={fee}
              placeholder="0"
              aria-label="Абонентская плата в долларах в месяц"
              aria-invalid={feeBad || undefined}
              onChange={(e) => setFee(e.target.value)}
            />
            <span>$ в месяц</span>
          </label>
        )}
        {feeBad ? (
          <p className="md-note tm-err">Плата — число от 0 до 100 000 $</p>
        ) : (
          kind === 'subscription' && (
            <p className="md-note">
              {feeNum !== null && feeNum > 0 && inRubles() && shownOfUsd(feeNum) !== null
                ? `Это ${fromUsdExact(feeNum)} в месяц по курсу хаба. Провайдеры берут плату в долларах, поэтому её вводят в $.`
                : 'Провайдеры берут плату в долларах, поэтому её вводят в $; в списке она пересчитана в рубли.'}
            </p>
          )
        )}
      </div>
      <div className="md-sec">
        <div className="md-sec__t">Кто платит</div>
        <Seg
          label="Кто платит"
          items={(Object.keys(FUNDING) as AccountFunding[]).map((k) => ({ id: k, label: FUNDING[k] }))}
          value={funding}
          onPick={setFunding}
        />
        <p className="md-note">
          {funding === 'org'
            ? 'Расход пойдёт в бюджет и лимиты команды того, кто работает.'
            : 'Расход останется личным: виден в аналитике, в лимиты не входит.'}
        </p>
      </div>
      <div className="md-sec">
        <div className="md-sec__t">Владелец</div>
        <Seg label="Владелец" items={ownerItems} value={owner} onPick={setOwner} wrap />
        {owner !== null && (
          <label className="ac-check">
            <input type="checkbox" checked={history} onChange={(e) => setHistory(e.target.checked)} />
            <span>{`Отнести к аккаунту прошлые чаты ${ownerName || 'владельца'}, где аккаунт не определился`}</span>
          </label>
        )}
      </div>
      {tenants.length > 1 && (
        <div className="md-sec">
          <div className="md-sec__t">Где доступен</div>
          <Seg
            label="Где доступен"
            items={[
              { id: null as string | null, label: 'Вся организация' },
              ...tenants.map((t) => ({ id: t.id as string | null, label: t.name })),
            ]}
            value={tenant}
            onPick={setTenant}
            wrap
          />
        </div>
      )}
      {problem && (
        <div className="md-sec">
          <p className="md-note tm-err" role="alert">
            {problem}
          </p>
        </div>
      )}
      <div className="md-actions tm-actions ac-actions">
        <span className="hx-spacer" />
        <Btn variant="ghost" size="sm" onClick={onCancel}>
          Отмена
        </Btn>
        <button
          type="submit"
          className="hx-btn md-save"
          data-variant="primary"
          data-size="sm"
          data-busy={busy ? '1' : undefined}
          aria-disabled={bad}
        >
          <span className="md-spin" aria-hidden="true" />
          {busy ? 'Сохраняем' : a.discovered ? 'Зарегистрировать' : 'Сохранить'}
        </button>
      </div>
    </form>
  );
};

// ── The screen ──────────────────────────────────────────────────────────────────────────────────────────────────────

const Skeleton = () => (
  <div className="mt" aria-busy="true">
    {Array.from({ length: 4 }, (_, i) => (
      <div key={i} className="pt-row ac-row">
        <span className="hx-sk" style={{ height: 32 }} />
      </div>
    ))}
  </div>
);

const ACC_SORTS: SortKey[] = [
  { id: 'spend', label: 'По расходу' },
  { id: 'name', label: 'По имени', first: 'asc' },
  { id: 'fee', label: 'По абонентской' },
];

const Screen = () => {
  const { shell, base } = useOrg();
  const view = useListView(ACC_SORTS, { key: null, dir: 'desc' });
  const { data, error, reload } = useLoad((api) => api.accounts(), []);
  const url = useUrl(CARDS);
  const [saved, setSaved] = useState<Record<string, Account>>({});
  const [notice, setNotice] = useState<string | null>(null);
  const canManage = shell.viewer.role === 'admin';
  const tenantName = (id: string | null): string =>
    id === null ? 'Вся организация' : (shell.tenants.find((t) => t.id === id)?.name ?? 'тенант');

  const list = (data?.accounts ?? []).map((a) => saved[a.id] ?? a);
  const sorted = sortRows(list, view.sort, {
    spend: (a) => a.monthRub,
    name: (a) => titleOf(a),
    fee: (a) => (a.kind === 'subscription' ? a.feeUsd : 0),
  });
  const matched = searchRows(sorted, view.q, (a) => [
    titleOf(a),
    a.label,
    providerName(a.provider),
    a.plan,
    ...a.people.map((x) => x.name),
  ]);
  const ordered = [...matched].sort((x, y) => Number(y.discovered) - Number(x.discovered));
  const found = ordered.filter((a) => a.discovered);
  const known = ordered.filter((a) => !a.discovered);

  const openId = url.get('a');
  const opened = openId ? list.find((a) => a.id === openId) : undefined;
  const [kept, setKept] = useState<Account | undefined>(opened);
  if (opened && opened !== kept) setKept(opened);
  const shown = opened ?? kept;
  const editing = !!shown && canManage && (url.get('dlg') === 'register' || shown.discovered);

  const totalRub = list.reduce((n, a) => n + a.monthRub, 0);
  const feesUsd = list.reduce((n, a) => n + (a.kind === 'subscription' && !a.discovered ? a.feeUsd : 0), 0);

  return (
    <>
      <div className="ad-head ac-head">
        <Titles
          title="Аккаунты"
          lead={
            data
              ? `${list.length} ${plural(list.length, 'аккаунт', 'аккаунта', 'аккаунтов')} · ${rub(totalRub)} по прайсу в этом месяце${feesUsd > 0 ? ` · абонентская ${fromUsdExact(feesUsd)} в месяц` : ''}`
              : 'Подписки и ключи, на которых работают агенты'
          }
        />
      </div>
      {notice && (
        <p className="ac-notice" role="status">
          <Icon i="check" size="sm" />
          {notice}
        </p>
      )}
      {error && !data ? (
        <p className="an-err" role="alert">
          <b>Аккаунты не загрузились</b>
          <span>{orgErrorText(error)}</span>
          <Btn size="sm" icon="rotate-cw" onClick={reload}>
            Повторить
          </Btn>
        </p>
      ) : !data ? (
        <Skeleton />
      ) : list.length === 0 ? (
        <p className="ov-empty">
          Аккаунтов пока нет: они появятся, когда агенты на машинах организации начнут работать.
        </p>
      ) : (
        <div className="ac-stack">
          <ListBar keys={ACC_SORTS} view={view} placeholder="Найти аккаунт или человека" />
          {ordered.length === 0 && <NoMatch view={view} />}
          {found.length > 0 && (
            <section className="ac-group" aria-label="Ждут регистрации">
              <h3 className="ac-group__t">
                Найдены на машинах <span className="at-title__count">{found.length}</span>
                <small>
                  Скажите, что это за аккаунт и кто за него платит — тогда расход попадёт в бюджет
                </small>
              </h3>
              <div className="mt">
                {found.map((a) => (
                  <Row
                    key={a.id}
                    a={a}
                    sub={subOf(a, tenantName)}
                    onOpen={() => url.open({ a: a.id, dlg: canManage ? 'register' : null })}
                  />
                ))}
              </div>
            </section>
          )}
          <section className="ac-group" aria-label="Аккаунты организации">
            {found.length > 0 && <h3 className="ac-group__t">Аккаунты организации</h3>}
            {known.length === 0 ? (
              ordered.length === 0 ? null : (
                <p className="ov-empty">Зарегистрированных аккаунтов пока нет.</p>
              )
            ) : (
              <div className="mt">
                <Head />
                {known.map((a) => (
                  <Row
                    key={a.id}
                    a={a}
                    sub={subOf(a, tenantName)}
                    onOpen={() => url.open({ a: a.id, dlg: null })}
                  />
                ))}
              </div>
            )}
          </section>
        </div>
      )}
      <Drawer
        open={!!opened}
        onClose={url.close}
        className="md ac"
        label={shown?.label ?? 'Аккаунт'}
        lead={
          <span
            className="ad-ic ac-lead"
            style={{ '--c': shown?.discovered ? '#c8830b' : '#8968f4' } as CSSProperties}
          >
            <Icon i={shown?.kind === 'api_key' ? 'key-round' : 'coins'} />
          </span>
        }
        title={
          shown ? (shown.discovered ? 'Незнакомый аккаунт' : editing ? 'Изменить аккаунт' : shown.label) : ''
        }
        sub={shown ? subOf(shown, tenantName) : undefined}
      >
        {shown &&
          (editing ? (
            <Form
              key={`${shown.id}:form`}
              a={shown}
              tenants={shell.tenants}
              onCancel={shown.discovered ? url.close : () => url.put({ dlg: null })}
              onDone={(next) => {
                if (next) setSaved((m) => ({ ...m, [next.id]: next }));
                setNotice(
                  shown.discovered
                    ? `«${next?.label ?? shown.label}» зарегистрирован: его расход теперь считается по правилам организации`
                    : 'Аккаунт сохранён',
                );
                url.close();
                reload();
              }}
            />
          ) : (
            <Card
              a={shown}
              tenant={tenantName(shown.tenantId)}
              canManage={canManage}
              base={base}
              onEdit={() => url.put({ dlg: 'register' })}
            />
          ))}
      </Drawer>
    </>
  );
};

export default Screen;
