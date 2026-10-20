import { useId, useRef, useState, type FormEvent } from 'react';
import dayjs from 'dayjs';
import useMinuteClock from '@/components/agents/sidebar/use-minute-clock';
import { KeyRound, MoreHorizontal, Plus } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHIP_TONE,
  CONFIRM_TONE,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwBadge,
  TwButton,
  TwConfirm,
  TwDialog,
  TwMenuButton,
  TwSpinner,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import useVaultEntries from '@/hooks/use-vault-entries';
import { compactAge } from '@/lib/hl-compact-time';
import {
  createVaultEntry,
  deleteVaultEntry,
  fetchVaultAudit,
  revokeVaultGrants,
  updateVaultEntry,
  vaultChanged,
} from '@/lib/hl-vault-client';
import { canonicalVaultOrigin } from '@/lib/vault/vault-origin';
import {
  VAULT_MAX_LABEL,
  VAULT_MAX_LOGIN,
  VAULT_MAX_PASSWORD,
  type IVaultAuditEvent,
  type IVaultEntry,
} from '@/types/vault';
import { t } from '@/lib/i18n';

type TForm = { kind: 'create'; canShare: boolean } | { kind: 'edit'; entry: IVaultEntry };
type TConfirm = { kind: 'delete' | 'revoke'; entry: IVaultEntry };

const failure = (error: unknown): string =>
  error instanceof Error ? error.message : t('vault.vaultTab.noAnswer');

const hostOf = (origin: string): string => origin.replace(/^https:\/\//, '');

const usedText = (entry: IVaultEntry, now: number): string => {
  if (!entry.lastUsedAt)
    return t('vault.vaultTab.addedOn', { date: dayjs(entry.createdAt).format('DD.MM.YYYY') });
  const at = Date.parse(entry.lastUsedAt);
  return now - at < 60_000
    ? t('vault.vaultTab.usedJustNow')
    : t('vault.vaultTab.usedAgo', { age: compactAge(at, now) });
};

interface IEntryFormProps {
  form: TForm;
  onClose: () => void;
}

/** The password lives in this form's state only until it is sent; the field is never filled from the safe. */
const EntryForm = ({ form, onClose }: IEntryFormProps) => {
  const editing = form.kind === 'edit';
  const formId = useId();
  const ids = { origin: useId(), login: useId(), password: useId(), label: useId(), shared: useId() };
  const firstRef = useRef<HTMLInputElement>(null);
  const [origin, setOrigin] = useState(editing ? form.entry.origin : '');
  const [login, setLogin] = useState(editing ? form.entry.login : '');
  const [password, setPassword] = useState('');
  const [label, setLabel] = useState(editing ? (form.entry.label ?? '') : '');
  const [shared, setShared] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const canonical = canonicalVaultOrigin(origin.trim());
  const valid = !!canonical && login.trim().length > 0 && (editing || password.length > 0);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!valid || pending || !canonical) {
      if (!canonical && origin) setError(t('vault.vaultTab.originHint'));
      return;
    }
    setPending(true);
    setError(null);
    try {
      if (form.kind === 'edit') {
        await updateVaultEntry(form.entry.id, {
          ...(login.trim() !== form.entry.login ? { login: login.trim() } : {}),
          ...(password ? { password } : {}),
          label: label.trim() || null,
        });
      } else {
        await createVaultEntry({ origin: canonical, login: login.trim(), password, label, shared });
      }
      setPassword('');
      vaultChanged();
      onClose();
    } catch (err) {
      setError(failure(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <TwDialog
      open
      onClose={onClose}
      dismissible={!pending}
      title={editing ? t('vault.vaultTab.editTitle') : t('vault.vaultTab.newTitle')}
      description={t('vault.vaultTab.formNote')}
      initialFocusRef={firstRef}
      className="tw-vault-form-dialog"
      footer={
        <>
          <TwButton variant={BUTTON_VARIANT.ghost} onClick={onClose} aria-disabled={pending || undefined}>
            {t('vault.vaultTab.cancel')}
          </TwButton>
          <TwButton
            variant={BUTTON_VARIANT.primary}
            type="submit"
            form={formId}
            pending={pending}
            disabled={!valid}
          >
            {t('vault.vaultTab.save')}
          </TwButton>
        </>
      }
    >
      <form
        id={formId}
        className="tw-sform tw-vault-form"
        autoComplete="off"
        onSubmit={(event) => void submit(event)}
      >
        <label htmlFor={ids.origin} className="tw-sform-label">
          {t('vault.vaultTab.site')}
        </label>
        <input
          ref={editing ? undefined : firstRef}
          id={ids.origin}
          className="tw-sform-input"
          value={origin}
          readOnly={editing}
          inputMode="url"
          placeholder="https://stand.example.com"
          spellCheck={false}
          autoComplete="off"
          aria-invalid={!!origin && !canonical}
          onChange={(event) => setOrigin(event.target.value)}
        />
        <label htmlFor={ids.login} className="tw-sform-label">
          {t('vault.vaultTab.login')}
        </label>
        <input
          ref={editing ? firstRef : undefined}
          id={ids.login}
          className="tw-sform-input"
          value={login}
          maxLength={VAULT_MAX_LOGIN}
          spellCheck={false}
          autoComplete="off"
          onChange={(event) => setLogin(event.target.value)}
        />
        <label htmlFor={ids.password} className="tw-sform-label">
          {editing ? t('vault.vaultTab.newPassword') : t('vault.vaultTab.password')}
        </label>
        <input
          id={ids.password}
          type="password"
          className="tw-sform-input"
          value={password}
          maxLength={VAULT_MAX_PASSWORD}
          autoComplete="new-password"
          onChange={(event) => setPassword(event.target.value)}
        />
        <label htmlFor={ids.label} className="tw-sform-label">
          {t('vault.vaultTab.label')}
        </label>
        <input
          id={ids.label}
          className="tw-sform-input"
          value={label}
          maxLength={VAULT_MAX_LABEL}
          placeholder={t('vault.vaultTab.labelPlaceholder')}
          autoComplete="off"
          onChange={(event) => setLabel(event.target.value)}
        />
        {form.kind === 'create' && form.canShare && (
          <label htmlFor={ids.shared} className="tw-vault-check">
            <input
              id={ids.shared}
              type="checkbox"
              checked={shared}
              onChange={(event) => setShared(event.target.checked)}
            />
            <span>{t('vault.vaultTab.sharedCheck')}</span>
          </label>
        )}
        {error && (
          <p role="alert" className="tw-ui-confirm-error">
            {error}
          </p>
        )}
      </form>
    </TwDialog>
  );
};

const ACTION_TEXT: Record<string, string> = {
  create: t('vault.vaultTab.actionCreate'),
  update: t('vault.vaultTab.actionUpdate'),
  'update-password': t('vault.vaultTab.actionUpdatePassword'),
  delete: t('vault.vaultTab.actionDelete'),
  reveal: t('vault.vaultTab.actionReveal'),
  approve: t('vault.vaultTab.actionApprove'),
  deny: t('vault.vaultTab.actionDeny'),
  revoke: t('vault.vaultTab.actionRevoke'),
};

const OUTCOME_TEXT: Record<string, string> = {
  ok: '',
  'origin-mismatch': t('vault.vaultTab.outcomeOriginMismatch'),
  'rate-limited': t('vault.vaultTab.outcomeRateLimited'),
  'needs-approval': t('vault.vaultTab.outcomeNeedsApproval'),
  'decrypt-failed': t('vault.vaultTab.outcomeDecryptFailed'),
  denied: '',
};

const AuditList = () => {
  const [events, setEvents] = useState<IVaultAuditEvent[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next) fetchVaultAudit().then(setEvents, (err: unknown) => setError(failure(err)));
  };
  return (
    <section className="tw-vault-audit" aria-label={t('vault.vaultTab.auditLabel')}>
      <button type="button" className="tw-vault-audit-toggle" aria-expanded={open} onClick={toggle}>
        {t('vault.vaultTab.audit')}
      </button>
      {open && error && (
        <p className="tw-rpane-muted" role="alert">
          {error}
        </p>
      )}
      {open && !error && events === null && <TwSpinner />}
      {open && events?.length === 0 && <p className="tw-rpane-muted">{t('vault.vaultTab.auditEmpty')}</p>}
      {open && !!events?.length && (
        <ol className="tw-vault-audit-list">
          {events.map((event, index) => (
            <li key={`${event.at}-${index}`} className="tw-vault-audit-row">
              <time dateTime={event.at}>{dayjs(event.at).format('DD.MM HH:mm')}</time>
              <span>
                {event.actor === 'agent' ? t('vault.vaultTab.actorAgent') : t('vault.vaultTab.actorYou')}
              </span>
              <span>
                {ACTION_TEXT[event.action] ?? event.action}
                {OUTCOME_TEXT[event.outcome] ? ` · ${OUTCOME_TEXT[event.outcome]}` : ''}
              </span>
              <span className="tw-vault-muted">{event.origin ? hostOf(event.origin) : ''}</span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
};

/**
 * The vault in the right panel: the person's passwords for our sites, listed without values. There is no way to
 * show or copy a password; it is used only by "Log in as …" in the browser tab and by an agent the person allowed.
 */
const VaultTab = () => {
  const { view, refresh } = useVaultEntries();
  const [form, setForm] = useState<TForm | null>(null);
  const [confirm, setConfirm] = useState<TConfirm | null>(null);
  const [pending, setPending] = useState(false);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const now = useMinuteClock();

  const runConfirm = async () => {
    if (!confirm) return;
    setPending(true);
    setConfirmError(null);
    try {
      if (confirm.kind === 'delete') await deleteVaultEntry(confirm.entry.id);
      else await revokeVaultGrants(confirm.entry.id);
      setConfirm(null);
      vaultChanged();
    } catch (error) {
      setConfirmError(failure(error));
    } finally {
      setPending(false);
    }
  };

  if (view.kind === 'loading')
    return (
      <div className="tw-vault">
        <TwSpinner />
      </div>
    );
  if (view.kind === 'off' || view.kind === 'failed') {
    return (
      <div className="tw-rpane-empty tw-vault-off">
        <KeyRound />
        <strong>{view.kind === 'off' ? t('vault.vaultTab.off') : t('vault.vaultTab.noAnswer')}</strong>
        <p>{view.kind === 'off' ? view.reason : view.message}</p>
        {view.kind === 'failed' && (
          <TwButton size={BUTTON_SIZE.sm} onClick={refresh}>
            {t('vault.vaultTab.retry')}
          </TwButton>
        )}
      </div>
    );
  }

  const menu = (entry: IVaultEntry): TTwMenuEntry[] => [
    ...(entry.mine
      ? [
          {
            kind: TW_MENU_KIND.action,
            id: 'edit',
            label: t('vault.vaultTab.edit'),
            onSelect: () => setForm({ kind: 'edit', entry }),
          },
        ]
      : []),
    {
      kind: TW_MENU_KIND.action,
      id: 'revoke',
      label: t('vault.vaultTab.revokeGrants'),
      onSelect: () => setConfirm({ kind: 'revoke', entry }),
    },
    ...(entry.mine
      ? [
          {
            kind: TW_MENU_KIND.action,
            id: 'delete',
            label: t('vault.vaultTab.delete'),
            onSelect: () => setConfirm({ kind: 'delete', entry }),
          },
        ]
      : []),
  ];

  return (
    <div className="tw-vault">
      <header className="tw-vault-head">
        <p className="tw-vault-intro">{t('vault.vaultTab.intro')}</p>
        <TwButton
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.primary}
          icon={<Plus />}
          onClick={() => setForm({ kind: 'create', canShare: view.status.canShare })}
        >
          {t('vault.vaultTab.add')}
        </TwButton>
      </header>
      {view.entries.length === 0 ? (
        <p className="tw-rpane-muted">{t('vault.vaultTab.empty')}</p>
      ) : (
        <ul className="tw-vault-list">
          {view.entries.map((entry) => (
            <li key={entry.id} className="tw-vault-row">
              <div className="tw-vault-row-main">
                <span className="tw-vault-host" title={entry.origin}>
                  {hostOf(entry.origin)}
                </span>
                <span className="tw-vault-login-name">
                  {entry.login}
                  {entry.label && <span className="tw-vault-muted"> · {entry.label}</span>}
                </span>
                <span className="tw-vault-meta">
                  <TwBadge tone={entry.shared ? CHIP_TONE.accent : CHIP_TONE.neutral}>
                    {entry.shared ? t('vault.vaultTab.shared') : t('vault.vaultTab.personal')}
                  </TwBadge>
                  <span className="tw-vault-muted">{usedText(entry, now)}</span>
                </span>
              </div>
              <TwMenuButton
                items={menu(entry)}
                menuLabel={`${hostOf(entry.origin)} · ${entry.login}`}
                align={LAYER_ALIGN.end}
                className="tw-icon-button"
                aria-label={t('vault.vaultTab.actionsFor', {
                  login: entry.login,
                  host: hostOf(entry.origin),
                })}
                title={t('vault.vaultTab.actions')}
              >
                <MoreHorizontal />
              </TwMenuButton>
            </li>
          ))}
        </ul>
      )}
      <AuditList />
      {form && <EntryForm form={form} onClose={() => setForm(null)} />}
      <TwConfirm
        open={!!confirm}
        tone={CONFIRM_TONE.danger}
        title={
          confirm?.kind === 'delete'
            ? t('vault.vaultTab.deleteConfirm', {
                login: confirm.entry.login,
                host: hostOf(confirm.entry.origin),
              })
            : t('vault.vaultTab.revokeConfirm')
        }
        description={
          confirm?.kind === 'delete' ? t('vault.vaultTab.deleteNote') : t('vault.vaultTab.revokeNote')
        }
        confirmLabel={confirm?.kind === 'delete' ? t('vault.vaultTab.delete') : t('vault.vaultTab.revoke')}
        pending={pending}
        error={confirmError}
        onConfirm={() => void runConfirm()}
        onCancel={() => {
          setConfirm(null);
          setConfirmError(null);
        }}
      />
    </div>
  );
};

export default VaultTab;
