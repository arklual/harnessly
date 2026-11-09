// The consent of an app where the person already is: a card in the chat whose agent installed it (over the
// field and in its right pane) and a bar over the app's own window. Everything or nothing: one «Разрешить» agrees to
// the whole list the card stands for, «Подробнее» opens the install screen with every line. The buttons are the host's,
// never the app's frame, and a click is the person's: an agent has no route to agree (internal/apps consent.go).
import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { Button } from '@/next/ui/button';
import { usePresence } from '@/next/ui/presence';
import ConsentSheet from '@/next/features/app-consent/consent-sheet';
import { say, threatsOf } from '@/next/features/app-consent/access-list';
import {
  consentApp,
  refreshInstalledApps,
  useConsentApps,
  type IAccess,
  type IInstalledApp,
} from '@/next/features/app-views/api';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import '@/next/styles/app-consent.css';

/** What one card asks: the app, the version it is for and what agreeing gives it. */
export interface IConsentAsk {
  app: IInstalledApp;
  version: string;
  access: readonly IAccess[];
  /** A newer version that reaches more: only what it adds is asked; the running version works meanwhile. */
  update: boolean;
}

/** What the app waits for, or null: a first install off until the word, or an update beside the running version. */
export const consentAskOf = (app: IInstalledApp): IConsentAsk | null => {
  if (app.pending) return { app, version: app.pending.version, access: app.pending.access, update: true };
  if (app.consent) return { app, version: app.version, access: app.access ?? [], update: false };
  return null;
};

/** How many lines of what the app gets the card names before «и ещё N». */
const SUMMARY_LINES = 3;

/** What the app gets, short: the most dangerous lines first, as many as fit. */
export const summaryOf = (access: readonly IAccess[]): string => {
  const order = ['critical', 'high', 'medium', 'low'];
  const lines = [...access]
    .sort((a, b) => order.indexOf(a.class) - order.indexOf(b.class))
    .map((a) => say(a.label));
  const shown = lines.slice(0, SUMMARY_LINES).join(', ');
  const rest = lines.length - SUMMARY_LINES;
  return rest > 0 ? t('next.consentAsk.more', { list: shown, n: rest }) : shown;
};

// «Не сейчас» hides a card for this page: per app and version, so a newer ask shows again.
const later = new Set<string>();
const laterListeners = new Set<() => void>();
let laterSeq = 0;
const putOff = (ask: IConsentAsk) => {
  later.add(`${ask.app.id}@${ask.version}`);
  laterSeq += 1;
  for (const f of laterListeners) f();
};
const useLater = () =>
  useSyncExternalStore(
    (f) => {
      laterListeners.add(f);
      return () => {
        laterListeners.delete(f);
      };
    },
    () => laterSeq,
  );

/** «Разрешить»: agrees to exactly what the card showed, then every screen reads the list again. */
const agree = async (machine: MachineId, ask: IConsentAsk) => {
  await consentApp(
    machine,
    ask.app.id,
    ask.version,
    ask.access.map((a) => a.key),
  );
  refreshInstalledApps(machine);
};

export const CONSENT_ASK_VARIANT = { dock: 'dock', pane: 'pane', stage: 'stage' } as const;
export type TConsentAskVariant = (typeof CONSENT_ASK_VARIANT)[keyof typeof CONSENT_ASK_VARIANT];

interface IConsentAskCardProps {
  machine: MachineId;
  ask: IConsentAsk;
  variant: TConsentAskVariant;
  /** «Не сейчас»; absent where the ask cannot be put off (over the app's window, which cannot run without it). */
  onLater?: () => void;
  closing?: boolean;
}

/**
 * How long «Разрешить» stays off after a card shows or changes. The bar stands over the app's window (an update's
 * running version), so an app could time a press of its own to land on a bar that has just appeared; a press must
 * come after the person could see what it agrees to.
 */
export const ARM_MS = 800;

/**
 * Whether the card's «Разрешить» takes a press yet: off for ARM_MS after `key` (the app, its version and what it gets)
 * shows, and again after the page was hidden (a press that lands when the person comes back must wait too). A blur of
 * the window is not counted: a press in the app's own window blurs the page, and the person's next press on the bar
 * would then always miss.
 */
const useArmed = (key: string) => {
  const [armed, setArmed] = useState<string | null>(null);
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const arm = () => {
      clearTimeout(timer);
      setArmed(null);
      if (!document.hidden) timer = setTimeout(() => setArmed(key), ARM_MS);
    };
    arm();
    document.addEventListener('visibilitychange', arm);
    return () => {
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', arm);
    };
  }, [key]);
  return armed === key;
};

export const ConsentAskCard = ({ machine, ask, variant, onLater, closing = false }: IConsentAskCardProps) => {
  const armed = useArmed(
    `${ask.app.id}@${ask.version}:${ask.update ? 'update' : 'install'}:${ask.access.map((a) => a.key).join(',')}`,
  );
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [details, setDetails] = useState(false);
  const threats = threatsOf(ask.access);
  const allow = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await agree(machine, ask);
      setDetails(false);
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const title = ask.update
    ? t('next.consentAsk.updateTitle', { name: ask.app.name })
    : t('next.consentAsk.title', { name: ask.app.name });
  return (
    <section
      className="hx-consentask"
      data-variant={variant}
      data-state={closing ? 'closing' : undefined}
      aria-label={title}
    >
      <div className="hx-consentask__head">
        <span className="hx-consentask__tile">
          <Icon name={ask.app.icon || 'layout-grid'} size="sm" />
        </span>
        <div className="hx-consentask__text">
          <strong className="hx-consentask__title">{title}</strong>
          {ask.access.length > 0 && <span className="hx-consentask__what">{summaryOf(ask.access)}</span>}
          {threats.length > 0 && <span className="hx-consentask__threat">{threats[0]}</span>}
        </div>
      </div>
      {failure && (
        <p className="hx-consentask__fail" role="alert">
          {failure}
        </p>
      )}
      <div className="hx-consentask__actions">
        {onLater && (
          <Button size="sm" variant="ghost" disabled={busy} onClick={onLater}>
            {t('next.consentAsk.later')}
          </Button>
        )}
        <Button size="sm" variant="ghost" disabled={busy || !armed} onClick={() => setDetails(true)}>
          {t('next.consentAsk.details')}
        </Button>
        <Button
          size="sm"
          variant="primary"
          busy={busy}
          disabled={!armed || closing}
          onClick={() => void allow()}
        >
          {t('next.consentAsk.allow')}
        </Button>
      </div>
      <ConsentSheet
        open={details}
        app={{
          name: ask.app.name,
          icon: ask.app.icon,
          version: ask.version,
          source: ask.app.source,
          dev: ask.app.dev,
        }}
        access={ask.access}
        update={ask.update}
        onConfirm={() => agree(machine, ask)}
        onCancel={() => setDetails(false)}
      />
    </section>
  );
};

/** As long as a card's exit in app-consent.css. */
const CARD_EXIT_MS = 180;

const LeavingCard = (props: Omit<IConsentAskCardProps, 'closing'> & { shown: boolean }) => {
  const { shown, ...card } = props;
  const presence = usePresence(shown ? card.ask : null, CARD_EXIT_MS);
  if (presence.value === null) return null;
  return <ConsentAskCard {...card} ask={presence.value} closing={presence.state === 'closing'} />;
};

interface IConsentAsksProps {
  machine: MachineId;
  /** The chat: only what its agent installed is asked here. */
  conversation: string;
  variant: typeof CONSENT_ASK_VARIANT.dock | typeof CONSENT_ASK_VARIANT.pane;
  /** This screen reads the list again by itself (one per page is enough: the others see the same list). */
  watch?: boolean;
}

/** The asks of a chat: every app its agent installed that waits for the person's word. */
export const ConsentAsks = ({ machine, conversation, variant, watch = false }: IConsentAsksProps) => {
  const waiting = useConsentApps(machine, true, watch);
  useLater();
  const asks = useMemo(
    () =>
      (waiting ?? [])
        .filter((a) => a.askedIn === conversation)
        .map(consentAskOf)
        .filter((a): a is IConsentAsk => a !== null),
    [waiting, conversation],
  );
  // An app that was just agreed to leaves with its exit: the ids seen keep a card mounted through it.
  const [seen, setSeen] = useState<Record<string, IConsentAsk>>({});
  const shownIds = new Set(
    asks
      .filter((a) => variant === CONSENT_ASK_VARIANT.pane || !later.has(`${a.app.id}@${a.version}`))
      .map((a) => a.app.id),
  );
  const fresh = asks.filter((a) => seen[a.app.id] !== a);
  if (fresh.length > 0) setSeen((s) => ({ ...s, ...Object.fromEntries(fresh.map((a) => [a.app.id, a])) }));
  const cards = Object.values(seen);
  if (cards.length === 0) return null;
  return (
    <div className="hx-consentasks" data-variant={variant}>
      {cards.map((ask) => (
        <LeavingCard
          key={ask.app.id}
          machine={machine}
          ask={ask}
          variant={variant}
          shown={shownIds.has(ask.app.id)}
          onLater={variant === CONSENT_ASK_VARIANT.dock ? () => putOff(ask) : undefined}
        />
      ))}
    </div>
  );
};

interface IConsentStageProps {
  machine: MachineId;
  app: IInstalledApp;
  /** The app's window; a first install that waits has none to run, so the bar stands on an empty stage. */
  children: ReactNode;
}

/** The app's window with the bar over its top right while it waits: «Разрешить» starts it in place. */
export const ConsentStage = ({ machine, app, children }: IConsentStageProps) => {
  const listed = consentAskOf(app);
  // While the list says it waits, the light read says whether it still does (agreed in the chat, or elsewhere).
  const waiting = useConsentApps(machine, listed !== null, true);
  const now = waiting === null ? app : (waiting.find((a) => a.id === app.id) ?? null);
  const ask = now === null ? null : consentAskOf(now);
  // Agreed somewhere else: the installed list is read again once, so the window starts here without a reload.
  const told = useRef<string | null>(null);
  const gone = listed !== null && waiting !== null && ask === null;
  useEffect(() => {
    const at = `${app.id}@${listed?.version ?? ''}`;
    if (!gone || told.current === at) return;
    told.current = at;
    refreshInstalledApps(machine);
  }, [gone, app.id, listed?.version, machine]);
  const presence = usePresence(ask, CARD_EXIT_MS);
  // The stage is there always, so the app's window keeps its place in the tree (and its session) when the bar leaves.
  return (
    <div className="hx-consentstage">
      {ask?.update === false ? <div className="hx-consentstage__empty" aria-hidden="true" /> : children}
      {presence.value !== null && (
        <div className="hx-consentstage__bar">
          <ConsentAskCard
            machine={machine}
            ask={presence.value}
            variant={CONSENT_ASK_VARIANT.stage}
            closing={presence.state === 'closing'}
          />
        </div>
      )}
    </div>
  );
};
