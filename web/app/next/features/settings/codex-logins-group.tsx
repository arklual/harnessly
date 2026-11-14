// «Ещё входы» on the Codex page: one more ChatGPT plan is one more sign-in of the CLI, with a name the person gives it.
// A chat starts on it from the model list of a new chat. The list is one card of rows: the name, whose plan
// it holds and what is left of it, «Войти» on one not signed in, and «⋯» for the rest (main, rename, sign out,
// remove); what ends something asks once in a fold under the row. «+ Добавить вход» at the foot opens the name in place.
// The list says whose plan a sign-in holds and never a token; the names stay on the machine.
import { Menu } from '@base-ui/react/menu';
import { useState, type FormEvent, type ReactNode } from 'react';
import type { ICodexLogin } from '@/next/features/settings/codex-logins-api';
import { Fold } from '@/next/features/settings/agent-account';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { usePresence } from '@/next/ui/presence';

/** A fold's exit, as long as hx-apage-fold-out in agent-page.css. */
const FOLD_EXIT_MS = 200;
const NAME_MAX = 40;

const FOLD = { signIn: 'sign-in', leave: 'leave', remove: 'remove' } as const;
type TFold = (typeof FOLD)[keyof typeof FOLD];

interface ICodexLoginCardProps {
  login: ICodexLogin;
  /** Resolves the words of a refusal, or null when the name was taken. */
  onRename: (name: string) => Promise<string | null>;
  onRemove: () => Promise<string | null>;
  /** Makes it the sign-in a new chat starts on, or gives that back. */
  onMain: (main: boolean) => Promise<string | null>;
  /** Signs it out; absent: the menu has no «Выйти». */
  onLogout?: () => Promise<string | null>;
  /** What is left of its plan, drawn under whose plan it is. */
  left?: ReactNode;
  /** The sign-in panel of this login. */
  children: ReactNode;
}

interface IRowMenuProps {
  login: ICodexLogin;
  busy: boolean;
  onMain: () => void;
  onRename: () => void;
  onLeave?: () => void;
  onRemove: () => void;
}

const RowMenu = ({ login, busy, onMain, onRename, onLeave, onRemove }: IRowMenuProps) => {
  const actions = useMenuActions();
  const label = t('next.codexLogins.menu', { name: login.name });
  const signed = login.account !== '';
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        disabled={busy}
        render={
          <button type="button" className="hx-apage__more" aria-label={label}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" sideOffset={6}>
        {signed && (
          <Menu.Item className="hx-cmenu__item" onClick={onMain}>
            <Icon name={login.main ? 'circle' : 'check'} size="sm" />
            <span className="hx-cmenu__txt">
              {login.main ? t('next.codexLogins.unmain') : t('next.codexLogins.makeMain')}
            </span>
          </Menu.Item>
        )}
        <Menu.Item className="hx-cmenu__item" onClick={onRename}>
          <Icon name="pencil" size="sm" />
          <span className="hx-cmenu__txt">{t('next.codexLogins.rename')}</span>
        </Menu.Item>
        {signed && onLeave && (
          <Menu.Item className="hx-cmenu__item" onClick={onLeave}>
            <Icon name="log-out" size="sm" />
            <span className="hx-cmenu__txt">{t('next.settings.agentPage.leave')}</span>
          </Menu.Item>
        )}
        <Menu.Separator className="hx-cmenu__sep" />
        <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={onRemove}>
          <Icon name="trash-2" size="sm" />
          <span className="hx-cmenu__txt">{t('next.codexLogins.remove')}</span>
        </Menu.Item>
      </MenuPanel>
    </Menu.Root>
  );
};

export const CodexLoginCard = ({
  login,
  onRename,
  onRemove,
  onMain,
  onLogout,
  left,
  children,
}: ICodexLoginCardProps) => {
  const [name, setName] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fold, setFold] = useState<TFold | null>(null);
  const shown = usePresence(fold, FOLD_EXIT_MS);
  const signed = login.account !== '';
  // A sign-in that has just gone through closes its panel.
  const [was, setWas] = useState(signed);
  if (was !== signed) {
    setWas(signed);
    if (signed && fold === FOLD.signIn) setFold(null);
  }

  const run = async (act: () => Promise<string | null>, done: () => void) => {
    setBusy(true);
    setError(null);
    const refused = await act();
    setBusy(false);
    if (refused === null) done();
    else setError(refused);
  };
  const editing = name !== null;
  const typed = (name ?? '').trim();
  const save = (event?: FormEvent) => {
    event?.preventDefault();
    if (typed === '' || typed === login.name) return setName(null);
    void run(
      () => onRename(typed),
      () => setName(null),
    );
  };
  const hint = signed ? login.account : t('next.codexLogins.off');

  return (
    <div className="hx-apage__login" data-on={signed}>
      <div className="hx-apage__login-head">
        {editing ? (
          <form className="hx-apage__inline" onSubmit={save}>
            <input
              className="hx-set__input"
              value={name}
              maxLength={NAME_MAX}
              autoFocus
              aria-label={t('next.codexLogins.name')}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') setName(null);
              }}
            />
            <SetButton tone="ghost" disabled={busy} onClick={() => setName(null)}>
              {t('next.providers.cancel')}
            </SetButton>
            <SetButton tone="primary" busy={busy} onClick={() => save()}>
              {t('next.codexLogins.save')}
            </SetButton>
          </form>
        ) : (
          <>
            <div className="hx-set__row-text">
              <span className="hx-set__row-label">{login.name}</span>
              <span className="hx-set__row-hint">
                {login.main ? `${hint} · ${t('next.codexLogins.mainHint')}` : hint}
              </span>
              {left}
            </div>
            {!signed && (
              <span className="hx-apage__connect" data-hidden={fold === FOLD.signIn || undefined}>
                <SetButton tone="ghost" onClick={() => setFold(FOLD.signIn)}>
                  {t('next.codexLogins.signIn')}
                </SetButton>
              </span>
            )}
            <RowMenu
              login={login}
              busy={busy}
              onMain={() =>
                void run(
                  () => onMain(!login.main),
                  () => undefined,
                )
              }
              onRename={() => setName(login.name)}
              onLeave={onLogout ? () => setFold(FOLD.leave) : undefined}
              onRemove={() => setFold(FOLD.remove)}
            />
          </>
        )}
      </div>
      {error !== null && <FormError className="hx-apage__err">{error}</FormError>}
      {shown.value !== null && (
        <Fold state={shown.state}>
          {shown.value === FOLD.signIn ? (
            <div className="hx-apage__panel">
              {children}
              <div className="hx-apage__actions">
                <SetButton tone="ghost" onClick={() => setFold(null)}>
                  {t('next.providers.cancel')}
                </SetButton>
              </div>
            </div>
          ) : (
            <div className="hx-apage__ask">
              <p className="hx-apage__ask-text">
                {shown.value === FOLD.remove ? t('next.codexLogins.removeSure') : t('next.login.leaveSure')}
              </p>
              <div className="hx-apage__actions">
                <SetButton tone="ghost" disabled={busy} onClick={() => setFold(null)}>
                  {t('next.settings.cancel')}
                </SetButton>
                <SetButton
                  tone="danger"
                  busy={busy}
                  onClick={() =>
                    void run(shown.value === FOLD.remove || !onLogout ? onRemove : onLogout, () =>
                      setFold(null),
                    )
                  }
                >
                  {shown.value === FOLD.remove
                    ? t('next.codexLogins.remove')
                    : t('next.settings.agentPage.leaveGo')}
                </SetButton>
              </div>
            </div>
          )}
        </Fold>
      )}
    </div>
  );
};

interface ICodexLoginsGroupProps {
  /** null while the list is read. */
  logins: readonly ICodexLogin[] | null;
  /** No more sign-ins fit. */
  full: boolean;
  onAdd: (name: string) => Promise<string | null>;
  renderLogin: (login: ICodexLogin) => ReactNode;
}

const CodexLoginsGroup = ({ logins, full, onAdd, renderLogin }: ICodexLoginsGroupProps) => {
  const [adding, setAdding] = useState(false);
  const shown = usePresence(adding ? true : null, FOLD_EXIT_MS);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  if (logins === null) return null;
  const typed = name.trim();
  const close = () => {
    setAdding(false);
    setName('');
    setError(null);
  };
  const add = (event?: FormEvent) => {
    event?.preventDefault();
    if (typed === '' || busy) return;
    setBusy(true);
    setError(null);
    void onAdd(typed).then((refused) => {
      setBusy(false);
      if (refused === null) close();
      else setError(refused);
    });
  };
  return (
    <Group title={t('next.codexLogins.title')} foot={t('next.codexLogins.foot')} card="hx-apage__logins">
      {logins.map(renderLogin)}
      {!full && (
        <div className="hx-apage__add">
          {shown.value === null && (
            <button type="button" className="hx-apage__add-btn" onClick={() => setAdding(true)}>
              <Icon name="plus" size="sm" />
              {t('next.codexLogins.add')}
            </button>
          )}
          {shown.value !== null && (
            <Fold state={shown.state}>
              <form className="hx-apage__inline" onSubmit={add}>
                <input
                  className="hx-set__input"
                  value={name}
                  maxLength={NAME_MAX}
                  autoFocus
                  placeholder={t('next.codexLogins.namePlaceholder')}
                  aria-label={t('next.codexLogins.name')}
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') close();
                  }}
                />
                <SetButton tone="ghost" disabled={busy} onClick={close}>
                  {t('next.providers.cancel')}
                </SetButton>
                <SetButton tone="primary" busy={busy} disabled={typed === ''} onClick={() => add()}>
                  {t('next.codexLogins.addGo')}
                </SetButton>
              </form>
              {error !== null && <FormError className="hx-apage__err">{error}</FormError>}
            </Fold>
          )}
        </div>
      )}
    </Group>
  );
};

export default CodexLoginsGroup;
