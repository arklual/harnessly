// «Модели и подписки»: the subscriptions and API keys the organization's agents work on — who pays, who used each
// this month and what it cost at list prices. An account the hub only found on a machine waits on top until an
// administrator says what it is; the card and its form live in the address (`?a=<id>`, `&dlg=register`).
import { type CSSProperties, type FormEvent, useState } from 'react';

import { navigate } from '../../lib/router';
import type { Account, AccountFunding, AccountKind, RegisterAccount } from '../../org/api/accounts';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { useLoad } from '../../org/hooks';
import { Head } from '../shell';
import { Btn, Drawer, Icon, initial } from '../ui';
import { inRubles, shownOfUsd } from '../../org/money';
import { fromUsdExact } from '../../org/format/overview';
import { plural, rub, usd } from './overview/fmt';
import { useUrl } from './teams/url';
import '../styles/accounts.css';

const CARDS = ['a', 'dlg', 'f'] as const;

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
  funding0,
  onDone,
  onCancel,
}: {
  a: Account;
  tenants: { id: string; name: string }[];
  /** The fixbox's choice: «сделать подпиской организации» or «разрешить как личную». */
  funding0?: AccountFunding;
  onDone: (saved: Account | null) => void;
  onCancel: () => void;
}) => {
  const { api } = useOrg();
  const plans = PLANS.filter((p) => p.provider === a.provider);
  const [label, setLabel] = useState(a.discovered ? '' : a.label);
  const [plan, setPlan] = useState(a.plan);
  const [kind, setKind] = useState<AccountKind>(a.kind);
  const [funding, setFunding] = useState<AccountFunding>(funding0 ?? (a.discovered ? 'org' : a.funding));
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
  <div className="t-card viz-card dash-tile hd-card flat" aria-busy="true">
    <table className="tbl">
      <tbody>
        {Array.from({ length: 4 }, (_, i) => (
          <tr key={i}>
            <td colSpan={6}>
              <span className="hx-sk" style={{ display: 'block', height: 28 }} />
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);

const KIND_PILL: Record<AccountKind, string> = {
  subscription: 'Подписка',
  api_key: 'Ключ API',
  prepaid: 'Предоплата',
};

/** The mockup's meter: the account's share of the biggest spend this month. */
const Meter = ({ pct }: { pct: number }) => (
  <span
    className="hd-meter"
    data-t="acc"
    style={{ '--w': `${Math.max(2, Math.min(100, pct)).toFixed(1)}%` } as CSSProperties}
  >
    <i />
  </span>
);

/** «Найдена подписка, которой нет в хабе»: an account seen on a machine that nobody registered yet. */
const FoundBox = ({
  a,
  canManage,
  onPick,
}: {
  a: Account;
  canManage: boolean;
  onPick: (f: AccountFunding | null) => void;
}) => {
  const who = a.owner?.name ?? a.people[0]?.name;
  const what = a.kind === 'subscription' ? 'личной подписке' : 'своём ключе';
  return (
    <div className="fixbox" style={{ marginBottom: 16 }}>
      <div className="row sp" style={{ flexWrap: 'wrap', gap: 10 }}>
        <div>
          <b>
            Найдена {a.kind === 'subscription' ? 'подписка' : 'ключ'}, которой нет в хабе:{' '}
            {a.label.split(' · ').pop()}
          </b>
          <div className="mut sm">
            {who ? `${who} работает на ${what} ${providerName(a.provider)}. ` : ''}Расход по ней не виден в
            бюджете команды.
          </div>
        </div>
        <div className="row">
          {canManage ? (
            <>
              <Btn className="hd-btn" variant="primary" size="sm" onClick={() => onPick('org')}>
                Сделать подпиской организации
              </Btn>
              <Btn className="hd-btn" variant="outline" size="sm" onClick={() => onPick('personal')}>
                Разрешить как личную
              </Btn>
            </>
          ) : (
            <Btn className="hd-btn" variant="outline" size="sm" onClick={() => onPick(null)}>
              Подробнее
            </Btn>
          )}
        </div>
      </div>
    </div>
  );
};

const Screen = () => {
  const { shell, base } = useOrg();
  const { data, error, reload } = useLoad((api) => api.accounts(), []);
  const url = useUrl(CARDS);
  const [saved, setSaved] = useState<Record<string, Account>>({});
  const [notice, setNotice] = useState<string | null>(null);
  const canManage = shell.viewer.role === 'admin';
  const tenantName = (id: string | null): string =>
    id === null ? 'Вся организация' : (shell.tenants.find((t) => t.id === id)?.name ?? 'тенант');

  const list = (data?.accounts ?? []).map((a) => saved[a.id] ?? a);
  const found = list.filter((a) => a.discovered);
  const known = list.filter((a) => !a.discovered).sort((x, y) => y.monthRub - x.monthRub);
  const top = Math.max(1, ...known.map((a) => a.monthRub));

  const openId = url.get('a');
  const opened = openId ? list.find((a) => a.id === openId) : undefined;
  const [kept, setKept] = useState<Account | undefined>(opened);
  if (opened && opened !== kept) setKept(opened);
  const shown = opened ?? kept;
  const editing = !!shown && canManage && (url.get('dlg') === 'register' || shown.discovered);
  const f = url.get('f');
  const funding0: AccountFunding | undefined = f === 'org' || f === 'personal' ? f : undefined;

  return (
    <>
      <Head
        title="Модели и подписки"
        lead="На чём работают агенты: подписки Claude и Codex, ключи API и свой шлюз моделей"
        acts={
          canManage ? (
            <Btn
              className="hd-btn"
              variant="primary"
              icon="plus"
              onClick={() => navigate(`${base}/env?add=provider`)}
            >
              Добавить подписку или ключ
            </Btn>
          ) : null
        }
      />
      {notice && (
        <p className="ac-notice" role="status">
          <Icon i="check" size="sm" />
          {notice}
        </p>
      )}
      {error && !data ? (
        <p className="an-err" role="alert">
          <b>Подписки не загрузились</b>
          <span>{orgErrorText(error)}</span>
          <Btn size="sm" icon="rotate-cw" onClick={reload}>
            Повторить
          </Btn>
        </p>
      ) : !data ? (
        <Skeleton />
      ) : list.length === 0 ? (
        <div className="t-card viz-card dash-tile hd-card">
          <div className="hd-empty">
            <span className="hd-empty__ic">
              <Icon i="key-round" />
            </span>
            <b>Ни одной подписки</b>
            <span>
              Без подписки агенты на машинах не смогут работать. Добавьте подписку Claude или Codex, или ключ
              API шлюза моделей.
            </span>
            {canManage && (
              <Btn className="hd-btn" variant="primary" onClick={() => navigate(`${base}/env?add=provider`)}>
                Добавить подписку
              </Btn>
            )}
          </div>
        </div>
      ) : (
        <>
          {found.map((a) => (
            <FoundBox
              key={a.id}
              a={a}
              canManage={canManage}
              onPick={(fd) => url.open({ a: a.id, dlg: canManage ? 'register' : null, f: fd })}
            />
          ))}
          {known.length > 0 && (
            <div className="t-card viz-card dash-tile hd-card flat">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Подписка или ключ</th>
                    <th>Вид</th>
                    <th>Расход за месяц</th>
                    <th>Кто платит</th>
                    <th>Кто на ней</th>
                    <th className="num">Платёж</th>
                  </tr>
                </thead>
                <tbody>
                  {known.map((a) => (
                    <tr
                      key={a.id}
                      className="click"
                      tabIndex={0}
                      onClick={() => url.open({ a: a.id, dlg: null, f: null })}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') url.open({ a: a.id, dlg: null, f: null });
                      }}
                    >
                      <td>
                        <div className="nm">{a.label}</div>
                        <div className="mut2 sm">
                          {[providerName(a.provider), planName(a) || null].filter(Boolean).join(' · ')}
                        </div>
                      </td>
                      <td>
                        <span className="pill">{KIND_PILL[a.kind]}</span>
                      </td>
                      <td style={{ width: '20%' }}>
                        <Meter pct={(a.monthRub / top) * 100} />
                        <div className="mut2 sm" style={{ marginTop: 4 }}>
                          {rub(a.monthRub)} по прайсу
                        </div>
                      </td>
                      <td>
                        <span className="mut sm">{FUNDING[a.funding]}</span>
                      </td>
                      <td>
                        <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
                          {a.people.length === 0 ? (
                            <span className="pill">{tenantName(a.tenantId).toLowerCase()}</span>
                          ) : (
                            <>
                              {a.people.slice(0, 2).map((p) => (
                                <span key={p.userId} className="pill">
                                  {p.name}
                                </span>
                              ))}
                              {a.people.length > 2 && <span className="pill">ещё {a.people.length - 2}</span>}
                            </>
                          )}
                        </div>
                      </td>
                      <td className="num">
                        {a.kind === 'subscription' && a.feeUsd > 0 ? `${fromUsdExact(a.feeUsd)}/мес` : '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
      <Drawer
        open={!!opened}
        onClose={url.close}
        className="md ac"
        label={shown?.label ?? 'Аккаунт'}
        lead={
          <span
            className="hb-ic ac-lead"
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
              funding0={funding0}
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
