// Settings → «Провайдеры» → «По API»: the model providers a person connects with a key, drawn as the cards
// of Codex and Claude Code above. The connected ones come first, each with its «⋯»; under them a card per
// provider still to connect, «Подключить» on its right, and the rarer ones folded under «Ещё провайдеры». «Подключить»
// folds the key out of that very card: the check runs before it adds, and says the provider's refusal in words. A
// connected card folds out its models, the check, the fields to change and the removal. Every list and line here is
// what the machine answered; nothing moves before it does.
import { Menu } from '@base-ui/react/menu';
import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type ReactNode } from 'react';
import EnvEditor, { envDirty, inputsOf, rowsOf, type IEnvRow } from '@/next/features/settings/ext-env-editor';
import ExtModelsEditor, {
  modelRowsOf,
  modelsDirty,
  modelsOf,
  type IModelRow,
} from '@/next/features/settings/ext-models-editor';
import {
  EXT_API,
  type IExtEnvInput,
  type IExtModel,
  type IExtPreset,
  type IExtProvider,
  type TExtApi,
} from '@/next/features/settings/ext-providers-api';
import { spendKey, type AccountSpendMap } from '@/next/features/limits/card-model';
import ProviderCard, {
  PROVIDER_CARD_STATE,
  type ProviderCardState,
} from '@/next/features/limits/provider-card';
import { API_PROFILE, profileLimitsOf, type MachineLimits } from '@/next/features/limits/model';
import { FormError, SegControl, SetButton } from '@/next/features/settings/rows';
import { plural, t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { usePresence, usePresentFlag, type TPresenceState } from '@/next/ui/presence';
import SecretField from '@/next/ui/secret-field';
import { Skeleton } from '@/next/ui/skeleton';

/** The preset of an address of the person's own (extproviders.PresetCustom). */
export const CUSTOM_PRESET = 'custom';

/** A fold's exit, as long as hx-extp-fold-out in ext-providers.css. */
const FOLD_EXIT_MS = 200;

/** How many providers to connect are shown before «Ещё провайдеры»; an address of the person's own is always under it. */
const SHOWN_OFFERS = 8;

/** The marks of the vendors the interface draws (styles/shell.css, .hx-agent); any other gets its first letter. */
const MARKS: Readonly<Record<string, string>> = {
  anthropic: 'claude',
  'kimi-code': 'kimi',
  kimi: 'kimi',
  glm: 'glm',
  'glm-cn': 'glm',
  xai: 'grok',
  [CUSTOM_PRESET]: 'own',
};

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

export interface IExtAdd {
  preset: IExtPreset;
  name: string;
  baseUrl: string;
  api: TExtApi;
  key: string;
}

export interface IExtEdit {
  name: string;
  /** Absent for a provider of a preset: its address and API are the preset's. */
  baseUrl?: string;
  api?: TExtApi;
  /** '' keeps the key the safe has. */
  key: string;
  /** The whole list of the launch's variables; absent when they were not touched. */
  env?: readonly IExtEnvInput[];
  /** The whole list of models; absent when they were not touched. */
  models?: readonly IExtModel[];
}

export type TExtProvidersState =
  | { k: 'loading' }
  | { k: 'failed'; text: string; onRetry: () => void }
  | {
      k: 'ready';
      providers: readonly IExtProvider[];
      presets: readonly IExtPreset[];
      /** The machine keeps several accounts of one provider. */
      accounts: boolean;
      /** The machine starts Claude Code chats on an Anthropic-compatible provider. */
      launch: boolean;
    };

interface IExtProvidersSectionProps {
  state: TExtProvidersState;
  /** The machine's limits: a provider whose vendor tells the balance of a key shows it in its row. */
  limits?: MachineLimits | null;
  /** What each account spent (the machine's report), by spendKey; absent or null while unknown. */
  spend?: AccountSpendMap | null;
  /** What each provider does now, by its id: the titles of the features given to it. null: not known yet. */
  usage: Readonly<Record<string, readonly string[]>> | null;
  /** Checks the connection and adds the provider: its id, or the words of why not. */
  onAdd: (draft: IExtAdd) => Promise<{ id: string } | { error: string }>;
  onSave: (id: string, edit: IExtEdit) => TDone;
  onRemove: (id: string) => TDone;
  /** Makes the account the default one of its provider. */
  onDefault: (id: string) => TDone;
  /** The check of a kept provider: whether it answered, and what to say. */
  onCheck: (id: string) => Promise<{ ok: boolean; text: string }>;
  /** Asks the provider for its models again. */
  onRefresh: (id: string) => TDone;
}

const apiChoices = () => [
  { value: EXT_API.openai, label: t('next.extProviders.api.openai') },
  { value: EXT_API.anthropic, label: t('next.extProviders.api.anthropic') },
];

const presetName = (preset: IExtPreset): string =>
  preset.id === CUSTOM_PRESET ? t('next.extProviders.custom') : preset.name;

const Logo = ({ preset, name }: { preset: string; name: string }) => {
  const mark = MARKS[preset];
  if (mark) return <i className="hx-agent" data-agent={mark} aria-hidden="true" />;
  return <span aria-hidden="true">{name.slice(0, 1).toLocaleUpperCase()}</span>;
};

/** The line under a provider to connect: what it gives. */
const offerLine = (preset: IExtPreset, launch: boolean): string => {
  if (preset.id === CUSTOM_PRESET) return t('next.extProviders.offer.custom');
  if (launch) return t('next.extProviders.offer.chats');
  return t('next.extProviders.offer');
};

/** The line under a provider's name: how many models it has and what it does. */
const lineOf = (provider: IExtProvider, used: readonly string[] | null): string => {
  const models =
    provider.models.length === 0
      ? t('next.extProviders.noModels')
      : plural(provider.models.length, 'next.extProviders.models');
  if (used === null) return models;
  const what = used.length === 0 ? t('next.extProviders.unused') : used.join(', ').toLocaleLowerCase();
  return `${models} · ${what}`;
};

const hostOf = (address: string): string | null => {
  try {
    return new URL(address.trim()).host;
  } catch {
    return null;
  }
};

/** Esc closes a fold, unless the machine is busy with what it asked. */
const escTo = (onClose: () => void, busy: boolean) => (event: KeyboardEvent) => {
  if (event.key !== 'Escape' || busy) return;
  event.stopPropagation();
  onClose();
};

/** What folds out of a card: it opens and closes by its height, and keeps its content while it closes. */
const Fold = ({ state, children }: { state: TPresenceState; children: ReactNode }) => (
  <div className="hx-extp__fold" data-state={state}>
    <div className="hx-extp__fold-in">{children}</div>
  </div>
);

/** The provider a new account is added to: the form starts with its address and API. */
interface IAddStart {
  baseUrl: string;
  api: TExtApi;
}

interface IAddFormProps {
  preset: IExtPreset;
  /** One more account of a provider the person has: it may be given a name of its own. */
  another: boolean;
  start: IAddStart | null;
  onAdd: (draft: IExtAdd) => TDone;
  onClose: () => void;
}

const AddForm = ({ preset, another, start, onAdd, onClose }: IAddFormProps) => {
  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState(start?.baseUrl ?? '');
  const [api, setApi] = useState<TExtApi>(start?.api ?? EXT_API.openai);
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const first = useRef<HTMLInputElement>(null);
  const custom = preset.id === CUSTOM_PRESET;
  const ready = key.trim() !== '' && (!custom || (name.trim() !== '' && baseUrl.trim() !== ''));
  useEffect(() => {
    first.current?.focus({ preventScroll: true });
  }, []);

  const submit = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!ready || busy) return;
    setBusy(true);
    setError(null);
    const refused = await onAdd({
      preset,
      name: custom || (another && name.trim() !== '') ? name.trim() : preset.name,
      baseUrl: custom ? baseUrl.trim() : preset.baseUrl,
      api: custom ? api : preset.api,
      key: key.trim(),
    });
    setBusy(false);
    setError(refused);
  };

  const keyField = (
    <SecretField
      ref={custom || another ? undefined : first}
      value={key}
      onChange={setKey}
      label={t('next.extProviders.key')}
      placeholder={
        custom
          ? t('next.extProviders.key.placeholder')
          : t('next.extProviders.key.placeholderOf', { name: preset.name })
      }
      disabled={busy}
      invalid={error !== null}
      onSubmit={() => void submit()}
    />
  );

  return (
    <form
      className="hx-extp__body"
      aria-busy={busy || undefined}
      aria-label={another ? t('next.extProviders.add.another', { name: preset.name }) : presetName(preset)}
      onSubmit={(event) => void submit(event)}
      onKeyDown={escTo(onClose, busy)}
    >
      {custom && (
        <>
          <div className="hx-extp__two">
            <label className="hx-extp__fld">
              <span className="hx-extp__label">{t('next.extProviders.name')}</span>
              <input
                ref={first}
                className="hx-set__input"
                value={name}
                disabled={busy}
                maxLength={60}
                placeholder={t('next.extProviders.name.placeholder')}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <label className="hx-extp__fld">
              <span className="hx-extp__label">{t('next.extProviders.address')}</span>
              <input
                className="hx-set__input hx-set__input--mono"
                value={baseUrl}
                disabled={busy}
                autoCapitalize="off"
                spellCheck={false}
                placeholder="https://llm.example.ru/v1"
                onChange={(event) => setBaseUrl(event.target.value)}
              />
            </label>
          </div>
          <div className="hx-extp__fld">
            <span className="hx-extp__label">{t('next.extProviders.api')}</span>
            <SegControl
              label={t('next.extProviders.api')}
              value={api}
              choices={apiChoices()}
              onPick={setApi}
              disabled={busy}
            />
            <small className="hx-extp__hint">{t('next.extProviders.api.hint')}</small>
          </div>
        </>
      )}
      {another && (
        <label className="hx-extp__fld">
          <span className="hx-extp__label">{t('next.extProviders.account')}</span>
          <input
            ref={first}
            className="hx-set__input"
            value={name}
            disabled={busy}
            maxLength={60}
            placeholder={t('next.extProviders.account.placeholder')}
            onChange={(event) => setName(event.target.value)}
          />
          <small className="hx-extp__hint">{t('next.extProviders.account.hint')}</small>
        </label>
      )}
      <div className="hx-extp__fld">
        {(custom || another) && <span className="hx-extp__label">{t('next.extProviders.key')}</span>}
        {keyField}
        <small className="hx-extp__hint">
          {t('next.extProviders.key.safe')}
          {preset.keyUrl !== '' && (
            <>
              {' · '}
              <a className="hx-extp__link" href={preset.keyUrl} target="_blank" rel="noreferrer">
                {t('next.extProviders.key.where')}
              </a>
            </>
          )}
        </small>
      </div>
      {error && <FormError>{error}</FormError>}
      <div className="hx-extp__btns">
        {busy ? (
          <span className="hx-extp__checking" role="status">
            <Icon name="loader-circle" size="sm" className="hx-extp__spin" />
            {t('next.extProviders.add.checking')}
          </span>
        ) : (
          <>
            <SetButton tone="primary" disabled={!ready} onClick={() => void submit()}>
              {t('next.extProviders.add.submit')}
            </SetButton>
            <SetButton tone="ghost" onClick={onClose}>
              {t('next.extProviders.cancel')}
            </SetButton>
          </>
        )}
      </div>
    </form>
  );
};

const EDIT_BUSY = {
  save: 'save',
  check: 'check',
  refresh: 'refresh',
  remove: 'remove',
  primary: 'primary',
} as const;
type TEditBusy = (typeof EDIT_BUSY)[keyof typeof EDIT_BUSY];

/** How a connected card folds out: as it is, with the key field in focus, or with the removal asked already. */
const EDIT_MODE = { edit: 'edit', key: 'key', remove: 'remove' } as const;
type TEditMode = (typeof EDIT_MODE)[keyof typeof EDIT_MODE];

interface IEditFormProps extends Pick<
  IExtProvidersSectionProps,
  'onSave' | 'onRemove' | 'onDefault' | 'onCheck' | 'onRefresh'
> {
  provider: IExtProvider;
  mode: TEditMode;
  /** The provider has other accounts: this one may be made the default, and says so when it is. */
  several: boolean;
  /** The features that run on this account, in words; '' when none does. */
  used: string;
  onClose: () => void;
  /** Claude Code chats start on this provider: the variables of the launch are edited here. */
  launch: boolean;
}

const EditForm = ({
  provider,
  mode,
  several,
  used,
  onSave,
  onRemove,
  onDefault,
  onCheck,
  onRefresh,
  onClose,
  launch,
}: IEditFormProps) => {
  const custom = provider.preset === '' || provider.preset === CUSTOM_PRESET;
  const managed = provider.managed;
  const [name, setName] = useState(provider.name);
  const [baseUrl, setBaseUrl] = useState(provider.baseUrl);
  const [api, setApi] = useState<TExtApi>(provider.api);
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState<TEditBusy | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checked, setChecked] = useState<{ ok: boolean; text: string } | null>(null);
  const [sure, setSure] = useState(mode === EDIT_MODE.remove);
  // The variables as they are being edited; null: as the machine keeps them.
  const [edited, setEdited] = useState<IEnvRow[] | null>(null);
  const keyRef = useRef<HTMLInputElement>(null);
  const env = edited ?? rowsOf(provider.env);
  const envChanged = edited !== null && envDirty(edited, provider.env);
  // The models as they are being edited; null: as the machine keeps them.
  const [modelRows, setModelRows] = useState<IModelRow[] | null>(null);
  const models = modelRows ?? modelRowsOf(provider.models);
  const modelsChanged = modelRows !== null && modelsDirty(modelRows, provider.models);
  useEffect(() => {
    if (mode === EDIT_MODE.key) keyRef.current?.focus({ preventScroll: true });
  }, [mode]);

  const moved = custom && hostOf(baseUrl) !== provider.host;
  const dirty =
    envChanged ||
    modelsChanged ||
    name.trim() !== provider.name ||
    key.trim() !== '' ||
    (custom && (baseUrl.trim() !== provider.baseUrl || api !== provider.api));

  const run = async (kind: TEditBusy, action: () => TDone) => {
    if (busy !== null) return;
    setBusy(kind);
    setError(null);
    setChecked(null);
    const refused = await action();
    setBusy(null);
    setError(refused);
    return refused;
  };
  const save = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!dirty) return;
    const refused = await run(EDIT_BUSY.save, () =>
      onSave(provider.id, {
        name: name.trim(),
        key: key.trim(),
        ...(custom ? { baseUrl: baseUrl.trim(), api } : {}),
        ...(envChanged ? { env: inputsOf(env) } : {}),
        ...(modelsChanged ? { models: modelsOf(models) } : {}),
      }),
    );
    if (refused === null) {
      setKey('');
      setModelRows(null);
      // What was typed for a secret goes with the form: the machine has it now.
      setEdited(null);
    }
  };
  const check = async () => {
    if (busy !== null) return;
    setBusy(EDIT_BUSY.check);
    setError(null);
    setChecked(null);
    const found = await onCheck(provider.id);
    setBusy(null);
    setChecked(found);
  };

  let keyHint = t('next.extProviders.key.kept');
  if (!provider.hasKey) keyHint = t('next.extProviders.key.none');
  if (moved) keyHint = t('next.extProviders.key.moved');

  return (
    <form
      className="hx-extp__body"
      aria-busy={busy !== null || undefined}
      aria-label={provider.name}
      onSubmit={(event) => void save(event)}
      onKeyDown={escTo(onClose, busy !== null)}
    >
      {managed && (
        <small className="hx-extp__hint">{t('next.extProviders.managedHint', { host: provider.host })}</small>
      )}
      {!managed && (
        <>
          <div className={custom ? 'hx-extp__two' : undefined}>
            <label className="hx-extp__fld">
              <span className="hx-extp__label">{t('next.extProviders.name')}</span>
              <input
                className="hx-set__input"
                value={name}
                maxLength={60}
                disabled={busy !== null}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            {custom && (
              <label className="hx-extp__fld">
                <span className="hx-extp__label">{t('next.extProviders.address')}</span>
                <input
                  className="hx-set__input hx-set__input--mono"
                  value={baseUrl}
                  autoCapitalize="off"
                  spellCheck={false}
                  disabled={busy !== null}
                  onChange={(event) => setBaseUrl(event.target.value)}
                />
              </label>
            )}
          </div>
          {custom && (
            <div className="hx-extp__fld">
              <span className="hx-extp__label">{t('next.extProviders.api')}</span>
              <SegControl
                label={t('next.extProviders.api')}
                value={api}
                choices={apiChoices()}
                onPick={setApi}
                disabled={busy !== null}
              />
            </div>
          )}
          <div className="hx-extp__fld">
            <span className="hx-extp__label">{t('next.extProviders.key')}</span>
            <SecretField
              ref={keyRef}
              value={key}
              onChange={setKey}
              label={t('next.extProviders.key')}
              placeholder={
                provider.hasKey ? t('next.extProviders.key.replace') : t('next.extProviders.key.placeholder')
              }
              disabled={busy !== null}
              onSubmit={() => void save()}
            />
            <small className="hx-extp__hint" data-warn={moved || !provider.hasKey || undefined}>
              {keyHint}
            </small>
          </div>
        </>
      )}
      <ExtModelsEditor rows={models} readOnly={managed} disabled={busy !== null} onChange={setModelRows} />
      {launch && !managed && <EnvEditor rows={env} disabled={busy !== null} onChange={setEdited} />}
      {sure && used !== '' && (
        <small className="hx-extp__hint" data-warn>
          {t(several ? 'next.extProviders.removeUsedSeveral' : 'next.extProviders.removeUsed', { used })}
        </small>
      )}
      {error && <FormError>{error}</FormError>}
      {checked && (
        <p className="hx-extp__checked" data-ok={checked.ok} role="status">
          <Icon name={checked.ok ? 'circle-check' : 'triangle-alert'} size="sm" />
          {checked.text}
        </p>
      )}
      <div className="hx-extp__btns">
        {!managed && (
          <SetButton
            tone="primary"
            busy={busy === EDIT_BUSY.save}
            disabled={!dirty || busy !== null}
            onClick={() => void save()}
          >
            {t('next.extProviders.save')}
          </SetButton>
        )}
        <SetButton
          tone="ghost"
          busy={busy === EDIT_BUSY.check}
          disabled={busy !== null || !provider.hasKey}
          onClick={() => void check()}
        >
          {t('next.extProviders.check')}
        </SetButton>
        <SetButton
          tone="ghost"
          icon="rotate-ccw"
          busy={busy === EDIT_BUSY.refresh}
          disabled={busy !== null || !provider.hasKey}
          onClick={() => void run(EDIT_BUSY.refresh, () => onRefresh(provider.id))}
        >
          {t('next.extProviders.refresh')}
        </SetButton>
        {several && !provider.isDefault && (
          <SetButton
            tone="ghost"
            busy={busy === EDIT_BUSY.primary}
            disabled={busy !== null}
            onClick={() => void run(EDIT_BUSY.primary, () => onDefault(provider.id))}
          >
            {t('next.extProviders.makeDefault')}
          </SetButton>
        )}
        <span className="hx-extp__gap" />
        {!managed && (
          <SetButton
            tone="danger"
            busy={busy === EDIT_BUSY.remove}
            disabled={busy !== null}
            onClick={() => {
              if (!sure) {
                setSure(true);
                return;
              }
              void run(EDIT_BUSY.remove, () => onRemove(provider.id));
            }}
          >
            {sure ? t('next.extProviders.removeSure') : t('next.extProviders.remove')}
          </SetButton>
        )}
      </div>
    </form>
  );
};

/** What is open: a provider to connect (by its preset), a connected one, or one more account of a connected one. */
type TOpen =
  { k: 'add'; preset: string } | { k: 'edit'; id: string; mode: TEditMode } | { k: 'another'; id: string };

interface ICardMenuProps {
  provider: IExtProvider;
  /** Absent where the machine keeps one account of a provider. */
  onAnother?: () => void;
  onOpen: (mode: TEditMode) => void;
}

/** A connected card's «⋯»: the key, one more account, the removal. */
const CardMenu = ({ provider, onAnother, onOpen }: ICardMenuProps) => {
  const actions = useMenuActions();
  const label = t('next.extProviders.menu', { name: provider.name });
  return (
    <MenuRoot actionsRef={actions}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-extp__more" aria-label={label}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" sideOffset={6}>
        {!provider.managed && (
          <Menu.Item className="hx-cmenu__item" onClick={() => onOpen(EDIT_MODE.key)}>
            <Icon name="key-round" size="sm" />
            <span className="hx-cmenu__txt">{t('next.extProviders.changeKey')}</span>
          </Menu.Item>
        )}
        <Menu.Item className="hx-cmenu__item" onClick={() => onOpen(EDIT_MODE.edit)}>
          <Icon name="settings-2" size="sm" />
          <span className="hx-cmenu__txt">{t('next.extProviders.settings')}</span>
        </Menu.Item>
        {onAnother && (
          <Menu.Item className="hx-cmenu__item" onClick={onAnother}>
            <Icon name="plus" size="sm" />
            <span className="hx-cmenu__txt">{t('next.extProviders.another')}</span>
          </Menu.Item>
        )}
        {!provider.managed && (
          <>
            <Menu.Separator className="hx-cmenu__sep" />
            <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={() => onOpen(EDIT_MODE.remove)}>
              <Icon name="trash-2" size="sm" />
              <span className="hx-cmenu__txt">{t('next.extProviders.remove')}</span>
            </Menu.Item>
          </>
        )}
      </MenuPanel>
    </MenuRoot>
  );
};

interface IConnectedCardProps extends Pick<
  IExtProvidersSectionProps,
  'onSave' | 'onRemove' | 'onDefault' | 'onCheck' | 'onRefresh' | 'limits' | 'spend'
> {
  provider: IExtProvider;
  /** What of this card is open; null: closed. */
  open: TOpen | null;
  fresh: boolean;
  several: boolean;
  used: string;
  sub: string;
  launch: boolean;
  /** The preset of a new account of this provider; null where the machine keeps one. */
  anotherOf: IExtPreset | null;
  onOpen: (open: TOpen | null) => void;
  onAdd: (draft: IExtAdd) => TDone;
}

const ConnectedCard = ({
  provider,
  open,
  fresh,
  several,
  used,
  sub,
  launch,
  anotherOf,
  limits,
  spend,
  onOpen,
  onAdd,
  ...actions
}: IConnectedCardProps) => {
  const shown = usePresence(open, FOLD_EXIT_MS);
  const own =
    limits && provider.preset
      ? profileLimitsOf(limits, `${API_PROFILE}${provider.preset}`, provider.id)
      : null;
  const toggle = () =>
    onOpen(open?.k === 'edit' ? null : { k: 'edit', id: provider.id, mode: EDIT_MODE.edit });
  let state: ProviderCardState = PROVIDER_CARD_STATE.on;
  if (!provider.hasKey) state = PROVIDER_CARD_STATE.off;
  else if (own?.rejected) state = PROVIDER_CARD_STATE.rejected;
  const close = () => onOpen(null);
  const fold = shown.value;
  return (
    <ProviderCard
      fresh={fresh}
      expanded={open?.k === 'edit'}
      logo={<Logo preset={provider.preset || CUSTOM_PRESET} name={provider.name} />}
      name={
        <>
          {provider.name}
          {provider.managed && <span className="hx-extp__tag">{t('next.extProviders.managed')}</span>}
          {several && provider.isDefault && (
            <span className="hx-extp__tag">{t('next.extProviders.default')}</span>
          )}
        </>
      }
      sub={sub}
      state={state}
      accounts={[
        {
          key: provider.id,
          name: provider.name,
          plan: own?.plan ?? null,
          limits: own,
          // A provider's chats run in Claude Code and are counted to its name.
          spend: spend?.get(spendKey('claude', provider.name)) ?? null,
        },
      ]}
      side={
        <CardMenu
          provider={provider}
          onOpen={(mode) => onOpen({ k: 'edit', id: provider.id, mode })}
          onAnother={anotherOf ? () => onOpen({ k: 'another', id: provider.id }) : undefined}
        />
      }
      onOpen={toggle}
      onAction={() => onOpen({ k: 'edit', id: provider.id, mode: EDIT_MODE.key })}
    >
      {fold?.k === 'edit' && (
        <Fold state={shown.state}>
          <EditForm
            key={fold.mode}
            provider={provider}
            mode={fold.mode}
            several={several}
            used={used}
            launch={launch}
            onClose={close}
            {...actions}
          />
        </Fold>
      )}
      {fold?.k === 'another' && anotherOf && (
        <Fold state={shown.state}>
          <AddForm
            preset={anotherOf}
            another={anotherOf.id !== CUSTOM_PRESET}
            start={{ baseUrl: provider.baseUrl, api: provider.api }}
            onAdd={onAdd}
            onClose={close}
          />
        </Fold>
      )}
    </ProviderCard>
  );
};

interface IOfferCardProps {
  preset: IExtPreset;
  open: boolean;
  launch: boolean;
  onOpen: (open: boolean) => void;
  onAdd: (draft: IExtAdd) => TDone;
}

/** A provider still to connect: what it gives, and «Подключить» that folds the key out of the card. */
const OfferCard = ({ preset, open, launch, onOpen, onAdd }: IOfferCardProps) => {
  const shown = usePresence(open ? preset : null, FOLD_EXIT_MS);
  const name = presetName(preset);
  return (
    <ProviderCard
      logo={<Logo preset={preset.id} name={name} />}
      name={name}
      sub={offerLine(preset, launch)}
      state={PROVIDER_CARD_STATE.off}
      accounts={[]}
      expanded={open}
      onOpen={() => onOpen(!open)}
      side={
        <span className="hx-extp__connect" data-hidden={open || undefined}>
          <SetButton tone="ghost" icon="plus" onClick={() => onOpen(true)}>
            <span className="hx-extp__connect-txt">{t('next.limits.card.connect')}</span>
          </SetButton>
        </span>
      }
    >
      {shown.value && (
        <Fold state={shown.state}>
          <AddForm
            preset={shown.value}
            another={false}
            start={null}
            onAdd={onAdd}
            onClose={() => onOpen(false)}
          />
        </Fold>
      )}
    </ProviderCard>
  );
};

const ExtProvidersSection = ({
  state,
  limits,
  spend,
  usage,
  onAdd,
  onSave,
  onRemove,
  onDefault,
  onCheck,
  onRefresh,
}: IExtProvidersSectionProps) => {
  const [open, setOpen] = useState<TOpen | null>(null);
  // The provider a form has just added: its card comes in marked, once.
  const [fresh, setFresh] = useState<string | null>(null);
  const [more, setMore] = useState(false);
  const providers = state.k === 'ready' ? state.providers : [];
  const presets = state.k === 'ready' ? state.presets : [];
  // A provider connected is reached by its card («⋯» → one more account); an address of the person's own is always
  // offered.
  const taken = new Set(providers.map((p) => p.preset));
  const offers = presets.filter((p) => p.id !== CUSTOM_PRESET && !taken.has(p.id));
  const shownOffers = offers.slice(0, SHOWN_OFFERS);
  const rest = [...offers.slice(SHOWN_OFFERS), ...presets.filter((p) => p.id === CUSTOM_PRESET)];
  // What is under «Ещё провайдеры» is open when the person opened it or one of its cards is open.
  const restOpen =
    more || shownOffers.length === 0 || rest.some((p) => open?.k === 'add' && open.preset === p.id);
  const restShown = usePresentFlag(restOpen, FOLD_EXIT_MS);

  if (state.k === 'loading')
    return (
      <div className="hx-set__group">
        <div className="hx-set__group-title">{t('next.extProviders.title')}</div>
        <Skeleton className="hx-set__sk" />
      </div>
    );
  if (state.k === 'failed')
    return (
      <div className="hx-set__group">
        <div className="hx-set__group-title">{t('next.extProviders.title')}</div>
        <div className="hx-set__card hx-extp__failed">
          <LoadFailed
            size={LOAD_FAILED_SIZE.inline}
            title={t('next.extProviders.loadFailed')}
            reason={state.text}
            onRetry={state.onRetry}
          />
        </div>
      </div>
    );

  const accountsOf = (group: string) => providers.filter((p) => p.group === group).length;
  const severalOf = (provider: IExtProvider) => state.accounts && accountsOf(provider.group) > 1;
  const usedBy = (provider: IExtProvider) => (usage?.[provider.id] ?? []).join(', ').toLocaleLowerCase();
  const presetOf = (provider: IExtProvider): IExtPreset | null =>
    presets.find((p) => p.id === (provider.preset || CUSTOM_PRESET)) ?? null;
  const added = async (draft: IExtAdd): TDone => {
    const made = await onAdd(draft);
    if ('error' in made) return made.error;
    setFresh(made.id);
    setOpen(null);
    return null;
  };
  const offer = (preset: IExtPreset) => (
    <OfferCard
      key={preset.id}
      preset={preset}
      open={open?.k === 'add' && open.preset === preset.id}
      launch={state.launch}
      onOpen={(on) => setOpen(on ? { k: 'add', preset: preset.id } : null)}
      onAdd={added}
    />
  );

  return (
    <div className="hx-set__group">
      <div className="hx-set__group-title">{t('next.extProviders.title')}</div>
      <div className="hx-extp__list">
        {providers.map((provider) => (
          <ConnectedCard
            key={provider.id}
            provider={provider}
            open={open !== null && open.k !== 'add' && open.id === provider.id ? open : null}
            fresh={provider.id === fresh}
            several={severalOf(provider)}
            used={usedBy(provider)}
            sub={
              provider.hasKey
                ? lineOf(provider, usage === null ? null : (usage[provider.id] ?? []))
                : t('next.extProviders.noKey')
            }
            launch={state.launch && provider.api === EXT_API.anthropic}
            anotherOf={state.accounts ? presetOf(provider) : null}
            limits={limits}
            spend={spend}
            onOpen={setOpen}
            onAdd={added}
            onSave={onSave}
            onRemove={onRemove}
            onDefault={onDefault}
            onCheck={onCheck}
            onRefresh={onRefresh}
          />
        ))}
        {shownOffers.map(offer)}
        {restShown.mounted && rest.length > 0 && (
          <Fold state={restShown.state}>
            <div className="hx-extp__list">{rest.map(offer)}</div>
          </Fold>
        )}
      </div>
      {shownOffers.length > 0 && rest.length > 0 && (
        <button
          type="button"
          className="hx-extp__toggle"
          aria-expanded={restOpen}
          onClick={() => {
            setMore(!restOpen);
            if (restOpen && open?.k === 'add') setOpen(null);
          }}
        >
          {restOpen ? t('next.extProviders.less') : t('next.extProviders.more')}
          <Icon name="chevron-down" size="sm" />
        </button>
      )}
      <div className="hx-set__foot">{t('next.extProviders.foot')}</div>
    </div>
  );
};

export default ExtProvidersSection;
