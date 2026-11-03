// «Connections» (Settings → Connections): the person's trackers, wikis and code hosts the agents read — one row each,
// its name above everything else, then the product, the address and who it signs in as. A row speaks of its state only
// when something is wrong, and of being the primary only when another connection does the same work. Its actions are
// in a menu: check again, make primary, rename, disconnect. Under the list, the paste field connects another one.
import { Menu } from '@base-ui/react/menu';
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import type { IConn } from '@/next/features/connections/api';
import ConnectForm, { type IConnectFormProps } from '@/next/features/connections/connect-form';
import ProductGlyph from '@/next/features/connections/glyphs';
import { hostOf, kindName, sharesWork, stateText } from '@/next/features/connections/model';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { useLeavingList } from '@/next/ui/presence';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/providers.css';
import '@/next/styles/connections.css';

/** A removed row folds for as long as .hx-reveal's transition. */
const LEAVE_MS = 280;
/** «Works» after «Check again» stays this long. */
const CHECKED_MS = 2500;
/** The list's stand-in while it is on its way: one array, so the leaving list sees no change. */
const NO_CONNS: readonly IConn[] = [];

const PANEL = { rename: 'rename', remove: 'remove' } as const;
type TPanel = (typeof PANEL)[keyof typeof PANEL];

const ACTION = { check: 'check', primary: 'primary', rename: 'rename', remove: 'remove' } as const;
type TAction = (typeof ACTION)[keyof typeof ACTION];

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

export interface IConnActions {
  onCheck: (id: string) => TDone;
  onPrimary: (id: string) => TDone;
  onRename: (id: string, name: string) => TDone;
  onRemove: (id: string) => TDone;
}

export interface IConnectionsSectionProps extends IConnActions, IConnectFormProps {
  /** null while the list is on its way. */
  connections: readonly IConn[] | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
}

const Reveal = ({ open, children }: { open: boolean; children: ReactNode }) => (
  <div className="hx-reveal" data-open={open}>
    <div className="hx-reveal__in" inert={!open || undefined}>
      {children}
    </div>
  </div>
);

/** «Jira · jira.acme.com · as Anna Petrova»: what it is, where, and whose account. */
const subLine = (conn: IConn): string => {
  const who = conn.me !== null && conn.me.name !== '' ? conn.me.name : conn.login;
  const where = t('next.connections.where', { product: kindName(conn.kind), host: hostOf(conn.baseUrl) });
  return who === '' ? where : t('next.connections.whereAs', { where, who });
};

interface IRowMenuProps {
  conn: IConn;
  canPrimary: boolean;
  disabled: boolean;
  onAction: (action: TAction) => void;
}

const RowMenu = ({ conn, canPrimary, disabled, onAction }: IRowMenuProps) => {
  const actions = useMenuActions();
  const label = t('next.connections.menu', { name: conn.name });
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        disabled={disabled}
        render={
          <button type="button" className="hx-conn-row__more" aria-label={label}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" sideOffset={6}>
        <Menu.Item className="hx-cmenu__item" onClick={() => onAction(ACTION.check)}>
          <Icon name="refresh-cw" size="sm" />
          <span className="hx-cmenu__txt">{t('next.connections.check')}</span>
        </Menu.Item>
        {canPrimary && (
          <Menu.Item className="hx-cmenu__item" onClick={() => onAction(ACTION.primary)}>
            <Icon name="pin" size="sm" />
            <span className="hx-cmenu__txt">{t('next.connections.makePrimary')}</span>
          </Menu.Item>
        )}
        {!conn.env && (
          <>
            <Menu.Item className="hx-cmenu__item" onClick={() => onAction(ACTION.rename)}>
              <Icon name="pencil" size="sm" />
              <span className="hx-cmenu__txt">{t('next.connections.rename')}</span>
            </Menu.Item>
            <Menu.Separator className="hx-cmenu__sep" />
            <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={() => onAction(ACTION.remove)}>
              <Icon name="trash-2" size="sm" />
              <span className="hx-cmenu__txt">{t('next.connections.disconnect')}</span>
            </Menu.Item>
          </>
        )}
      </MenuPanel>
    </Menu.Root>
  );
};

interface IRowProps {
  conn: IConn;
  all: readonly IConn[];
  leaving: boolean;
  actions: IConnActions;
}

const ConnRow = ({ conn, all, leaving, actions }: IRowProps) => {
  const [panel, setPanel] = useState<TPanel | null>(null);
  const [draft, setDraft] = useState(conn.name);
  const [busy, setBusy] = useState<TAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [kept, setKept] = useState('');
  const [checked, setChecked] = useState(false);
  const alive = useRef(true);
  const field = useRef<HTMLInputElement>(null);
  const ids = useId();
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (panel === PANEL.rename) field.current?.focus();
  }, [panel]);
  useEffect(() => {
    if (!checked) return;
    const timer = window.setTimeout(() => setChecked(false), CHECKED_MS);
    return () => window.clearTimeout(timer);
  }, [checked]);
  if (error !== null && kept !== error) setKept(error);

  const shared = sharesWork(conn, all);
  const problem = stateText(conn);

  const run = async (action: TAction, call: () => TDone): Promise<boolean> => {
    setBusy(action);
    setError(null);
    setChecked(false);
    const refused = await call();
    if (!alive.current) return false;
    setBusy(null);
    setError(refused);
    return refused === null;
  };

  const pick = (action: TAction) => {
    if (action === ACTION.check) {
      void run(action, () => actions.onCheck(conn.id)).then((ok) => alive.current && setChecked(ok));
      return;
    }
    if (action === ACTION.primary) {
      void run(action, () => actions.onPrimary(conn.id));
      return;
    }
    setError(null);
    if (action === ACTION.rename) setDraft(conn.name);
    setPanel(action === ACTION.rename ? PANEL.rename : PANEL.remove);
  };

  const rename = async () => {
    const next = draft.trim();
    if (next === '' || next === conn.name) {
      setPanel(null);
      return;
    }
    if (await run(ACTION.rename, () => actions.onRename(conn.id, next))) setPanel(null);
  };

  const close = () => {
    setPanel(null);
    setError(null);
  };

  return (
    <div className="hx-reveal hx-conn-wrap" data-open={!leaving}>
      <div className="hx-reveal__in">
        <div
          className="hx-conn-row"
          data-busy={busy !== null || undefined}
          aria-busy={busy !== null || undefined}
        >
          <div className="hx-conn-row__head">
            <ProductGlyph kind={conn.kind} />
            <span className="hx-conn-row__text">
              <span className="hx-conn-row__name">{conn.name}</span>
              <span className="hx-conn-row__sub">{subLine(conn)}</span>
              {conn.env && <span className="hx-conn-row__sub">{t('next.connections.env')}</span>}
              {problem !== null && (
                <span className="hx-conn-row__state">
                  <Icon name="triangle-alert" size="sm" />
                  {problem}
                </span>
              )}
              {checked && problem === null && (
                <span className="hx-conn-row__ok">
                  <Icon name="check" size="sm" />
                  {t('next.connections.works')}
                </span>
              )}
            </span>
            {conn.primary && shared && (
              <span className="hx-conn-row__primary">{t('next.connections.primary')}</span>
            )}
            {busy !== null && <Icon name="loader-circle" size="sm" className="hx-conn-row__spin" />}
            <RowMenu
              conn={conn}
              canPrimary={shared && !conn.primary}
              disabled={leaving || busy !== null}
              onAction={pick}
            />
          </div>
          <Reveal open={panel === PANEL.rename && !leaving}>
            <div className="hx-conn-row__panel">
              <label className="hx-conn-form__label" htmlFor={`${ids}-name`}>
                {t('next.connections.name')}
              </label>
              <input
                id={`${ids}-name`}
                ref={field}
                className="hx-set__input"
                value={draft}
                maxLength={80}
                autoComplete="off"
                disabled={busy !== null}
                onChange={(event) => {
                  setDraft(event.target.value);
                  setError(null);
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') void rename();
                  if (event.key === 'Escape') close();
                }}
              />
              <div className="hx-conn-row__buttons">
                <SetButton
                  tone="primary"
                  busy={busy === ACTION.rename}
                  disabled={draft.trim() === ''}
                  onClick={() => void rename()}
                >
                  {t('next.connections.save')}
                </SetButton>
                <SetButton tone="ghost" onClick={close}>
                  {t('next.connections.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={panel === PANEL.remove && !leaving}>
            <div className="hx-conn-row__panel">
              <p className="hx-conn-row__confirm">
                {t('next.connections.disconnectSure', { name: conn.name })}
              </p>
              <div className="hx-conn-row__buttons">
                <SetButton
                  tone="danger"
                  busy={busy === ACTION.remove}
                  onClick={() => void run(ACTION.remove, () => actions.onRemove(conn.id))}
                >
                  {t('next.connections.disconnect')}
                </SetButton>
                <SetButton tone="ghost" disabled={busy === ACTION.remove} onClick={close}>
                  {t('next.connections.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={error !== null && !leaving}>
            <div className="hx-conn-row__err">
              <FormError>{kept}</FormError>
            </div>
          </Reveal>
        </div>
      </div>
    </div>
  );
};

/** Bars of a row's size while the list is on its way. */
const RowsSkeleton = () => (
  <Delayed>
    <div className="hx-conn-sk">
      <Skeleton className="hx-conn-sk__row" />
      <Skeleton className="hx-conn-sk__row" />
    </div>
  </Delayed>
);

const ConnectionsSection = ({
  connections,
  failed = null,
  onDetect,
  onConnect,
  ...actions
}: IConnectionsSectionProps) => {
  const rows = useLeavingList(connections ?? NO_CONNS, (conn) => conn.id, LEAVE_MS);
  if (connections === null)
    return (
      <Group>
        {failed === null && <RowsSkeleton />}
        {failed !== null && (
          <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
        )}
      </Group>
    );
  const empty = connections.length === 0;
  return (
    <>
      {rows.length > 0 && (
        <Group title={t('next.connections.title')}>
          {rows.map(({ item, leaving }) => (
            <ConnRow key={item.id} conn={item} all={connections} leaving={leaving} actions={actions} />
          ))}
        </Group>
      )}
      <Group title={empty ? undefined : t('next.connections.add')} foot={t('next.connections.foot')}>
        {empty && <p className="hx-conn-empty">{t('next.connections.empty')}</p>}
        <ConnectForm onDetect={onDetect} onConnect={onConnect} />
      </Group>
    </>
  );
};

export default ConnectionsSection;
