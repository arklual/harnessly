// A provider's card in Settings → «Провайдеры» (the board «Лимиты — карточки провайдеров», variant A
// «Полоски»): the name and what it is, the forecast in one line on the right, and under it each account: a bar per
// window of what is left with the reset and, on the week, the pace mark; what the account spent today and this week.
// A key paid by the token shows its balance instead. A provider not connected is a dashed card with «Подключить», a
// refused key one line and «Заменить ключ». What the machine does not say is not drawn.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import {
  CARD_LEVEL,
  balanceForecastOf,
  cardForecastOf,
  cardWindowsOf,
  usd,
  type AccountSpend,
  type CardForecast,
  type CardWindow,
} from '@/next/features/limits/card-model';
import { money, type ProviderLimits } from '@/next/features/limits/model';
import { t } from '@/next/i18n';
import '@/next/styles/provider-card.css';

/** The spans are told to the minute at most. */
const TICK_MS = 60_000;

export interface IProviderCardAccount {
  key: string;
  /** Shown above the account's windows when the card has more than one account. */
  name: string;
  /** The plan beside the name («Max», «Plus»); null when unknown. */
  plan: string | null;
  limits: ProviderLimits | null;
  spend: AccountSpend | null;
}

export const PROVIDER_CARD_STATE = { on: 'on', off: 'off', rejected: 'rejected' } as const;
export type ProviderCardState = (typeof PROVIDER_CARD_STATE)[keyof typeof PROVIDER_CARD_STATE];

interface IProviderCardProps {
  logo: ReactNode;
  /** The name, with its tags («по умолчанию») where the list has them. */
  name: ReactNode;
  /** The line under the name: the plan, the key's last digits, «Не подключено». */
  sub: string;
  state: ProviderCardState;
  accounts: readonly IProviderCardAccount[];
  /** The head opens the provider (its page or its form); absent: the head is not a button. */
  onOpen?: () => void;
  /** «Подключить» of a card not connected, «Заменить ключ» of a refused key. */
  onAction?: () => void;
  /** The form has just added it: marked once. */
  fresh?: boolean;
  /** The head opens a form under the card that is open now. */
  expanded?: boolean;
  /** Beside the head, on its right: an action of the card's own («Подключить», its «⋯»). */
  side?: ReactNode;
  /** Under everything: what folds out of the card (a form of its own). */
  children?: ReactNode;
}

/** The bar of a window: what is left, coloured when it runs out, and the pace mark. */
const Bar = ({ win }: { win: CardWindow }) => {
  const ref = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--left', `${win.left}%`);
    if (win.pace === null) ref.current?.style.removeProperty('--pace');
    else ref.current?.style.setProperty('--pace', `${win.pace}%`);
  }, [win.left, win.pace]);
  return (
    <span
      ref={ref}
      className="hx-pcard__bar"
      role="meter"
      aria-label={win.label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={win.left}
    >
      <i />
      {win.pace !== null && <u title={t('next.limits.card.pace', { pace: win.pace })} />}
    </span>
  );
};

/** The days of the week as bars, today brightest. */
const Spark = ({ days }: { days: readonly number[] }) => {
  const ref = useRef<HTMLSpanElement>(null);
  const top = Math.max(...days);
  useLayoutEffect(() => {
    ref.current?.querySelectorAll<HTMLElement>('i').forEach((bar, i) => {
      bar.style.setProperty('--h', `${top > 0 ? Math.max(8, Math.round((days[i] / top) * 100)) : 8}%`);
    });
  }, [days, top]);
  return (
    <span ref={ref} className="hx-pcard__spark" role="img" aria-label={t('next.limits.card.spark')}>
      {days.map((_, i) => (
        <i key={i} />
      ))}
    </span>
  );
};

const Spend = ({ spend, week }: { spend: AccountSpend; week: string }) => (
  <div className="hx-pcard__spend">
    <span>
      {t('next.limits.card.today')} <b>{usd(spend.today)}</b>
    </span>
    <span>
      {week} <b>{usd(spend.week)}</b>
    </span>
  </div>
);

const Forecast = ({ fc, className }: { fc: CardForecast | null; className: string }) =>
  fc ? (
    <span className={className} data-level={fc.level}>
      {fc.text}
    </span>
  ) : null;

const Head = ({ head, expanded, onOpen }: { head: ReactNode; expanded?: boolean; onOpen?: () => void }) =>
  onOpen ? (
    <button type="button" className="hx-pcard__head" aria-expanded={expanded} onClick={onOpen}>
      {head}
    </button>
  ) : (
    <div className="hx-pcard__head">{head}</div>
  );

const ProviderCard = ({
  logo,
  name,
  sub,
  state,
  accounts,
  onOpen,
  onAction,
  fresh,
  expanded,
  side,
  children,
}: IProviderCardProps) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, []);
  const nowSec = now / 1000;
  const on = state === PROVIDER_CARD_STATE.on;

  const shown = on
    ? accounts.map((account) => ({ account, windows: cardWindowsOf(account.limits, nowSec) }))
    : [];
  // A key paid by the token: its balance stands for the windows (one account per such card).
  const paid = shown.length === 1 && shown[0].windows.length === 0 ? shown[0].account : null;
  const balance = paid?.limits?.balance ?? null;
  const forecast = balance
    ? balanceForecastOf(balance.amount, paid?.spend ?? null)
    : cardForecastOf(
        shown.flatMap((s) => s.windows),
        nowSec,
      );
  const several = shown.length > 1;

  const head = (
    <>
      <span className="hx-pcard__logo">{logo}</span>
      <span className="hx-pcard__title">
        <span className="hx-pcard__name">{name}</span>
        <span className="hx-pcard__sub">{sub}</span>
      </span>
      <Forecast fc={forecast} className="hx-pcard__fc" />
    </>
  );

  return (
    <div className="hx-pcard" data-state={state} data-fresh={fresh || undefined}>
      {side ? (
        <div className="hx-pcard__top">
          <Head head={head} expanded={expanded} onOpen={onOpen} />
          {side}
        </div>
      ) : (
        <Head head={head} expanded={expanded} onOpen={onOpen} />
      )}
      <Forecast fc={forecast} className="hx-pcard__fcm" />
      {balance && paid && (
        <div className="hx-pcard__bal">
          <span className="hx-pcard__sum">{money(balance)}</span>
          <span className="hx-pcard__bal-note">{t('next.limits.card.onBalance')}</span>
          {paid.spend && paid.spend.week > 0 && <Spark days={paid.spend.days} />}
        </div>
      )}
      {balance && paid?.spend && <Spend spend={paid.spend} week={t('next.limits.card.days7')} />}
      {!balance &&
        shown.map(({ account, windows }) => {
          const spend = account.spend && account.spend.week > 0 ? account.spend : null;
          if (windows.length === 0 && !spend) return null;
          return (
            <div key={account.key} className="hx-pcard__acc">
              {several && (
                <div className="hx-pcard__acc-name">
                  <b>{account.name}</b>
                  {account.plan && <span>{account.plan}</span>}
                </div>
              )}
              {windows.map((w) => (
                <div key={w.kind} className="hx-pcard__win" data-level={w.level}>
                  <span className="hx-pcard__win-label">{w.label}</span>
                  <Bar win={w} />
                  <span className="hx-pcard__win-left">{t('next.limits.card.left', { left: w.left })}</span>
                  <span className="hx-pcard__win-reset">{w.reset}</span>
                </div>
              ))}
              {spend && <Spend spend={spend} week={t('next.limits.card.week')} />}
            </div>
          );
        })}
      {state !== PROVIDER_CARD_STATE.on && onAction && (
        <div
          className="hx-pcard__note"
          data-level={state === PROVIDER_CARD_STATE.rejected ? CARD_LEVEL.bad : CARD_LEVEL.ok}
        >
          {state === PROVIDER_CARD_STATE.rejected && <span>{t('next.limits.card.rejected')}</span>}
          <button type="button" className="hx-set__btn" data-tone="ghost" onClick={onAction}>
            {state === PROVIDER_CARD_STATE.rejected
              ? t('next.limits.card.replaceKey')
              : t('next.limits.card.connect')}
          </button>
        </div>
      )}
      {children}
    </div>
  );
};

export default ProviderCard;
