// Connectors in Settings → Connections (internal/connectors, docs/connectors.md): the person's mail, calendars, chat
// bots, folders, feeds and webhooks. Above, what is connected: a row each, its name above everything, what it is and
// whose account under it, and words only when something is wrong. Below, the catalog: a card per connector grouped by
// kind; a card opens its form, drawn from the fields the connector describes, and the machine checks the values
// against the service before anything is kept.
import { useEffect, useId, useMemo, useState, type ReactNode } from 'react';
import {
  CONNECTION_STATE,
  CONNECTOR_KINDS,
  FIELD_TYPE,
  fieldShown,
  type IChoice,
  type IConnectorConn,
  type IConnectorField,
  type IConnectorRule,
  type IConnectorRuleInput,
  type IConnectorInput,
  type IConnectorShown,
  type IConnectorSpec,
  type IDiscoverInput,
  type TConnectorKind,
} from '@/next/features/connectors/api';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import Switch from '@/next/ui/switch';
import { useLeavingList } from '@/next/ui/presence';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/connections.css';
import '@/next/styles/connectors.css';

/** A removed row folds for as long as .hx-reveal's transition. */
const LEAVE_MS = 280;
/** «Works» after «Check» stays this long. */
const CHECKED_MS = 2500;
const NO_CONNS: readonly IConnectorConn[] = [];
const NO_RULED: readonly string[] = [];

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

const KIND_ICON: Record<TConnectorKind, string> = {
  mail: 'mail',
  calendar: 'calendar',
  chat: 'message-square',
  files: 'folder',
  feed: 'rss',
  webhook: 'link',
};

export interface IConnectorsSectionProps {
  /** null while the list is on its way. */
  catalog: readonly IConnectorSpec[] | null;
  connections: readonly IConnectorConn[] | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  onConnect: (input: IConnectorInput) => TDone;
  /** The choices of a discovered field (an account's calendars), or the words of why not. */
  onDiscover?: (input: IDiscoverInput) => Promise<readonly IChoice[] | string>;
  onCheck: (id: string) => TDone;
  onRemove: (id: string) => TDone;
  /** New values of a connection's generated secrets (a webhook's key); shown once in its row. */
  onReissue?: (id: string) => TDone;
  /** The default rules of a connection, or the words of why not; without it the rows have no «What to do». */
  onRules?: (id: string) => Promise<readonly IConnectorRule[] | string>;
  onSetRule?: (id: string, input: IConnectorRuleInput) => Promise<readonly IConnectorRule[] | string>;
  /** The connectors that have default rules. */
  ruled?: readonly string[];
}

type TRulesIO = {
  load: (id: string) => Promise<readonly IConnectorRule[] | string>;
  set: (id: string, input: IConnectorRuleInput) => Promise<readonly IConnectorRule[] | string>;
};

const wordsOf = (text: string): string[] =>
  text
    .split(',')
    .map((w) => w.trim())
    .filter((w) => w !== '');

/** One default rule: its name and what it does, a switch, and the words when it has them and is on. */
const RuleLine = ({
  rule,
  busy,
  onSet,
}: {
  rule: IConnectorRule;
  busy: boolean;
  onSet: (input: IConnectorRuleInput) => void;
}) => {
  const id = useId();
  const [draft, setDraft] = useState<string | null>(null);
  const shown = draft ?? (rule.keywords ?? []).join(', ');
  const save = () => {
    if (draft === null) return;
    setDraft(null);
    onSet({ rule: rule.id, keywords: wordsOf(draft) });
  };
  return (
    <div className="hx-cx-rule">
      <span className="hx-cx-rule__text">
        <span className="hx-cx-rule__title">{rule.title}</span>
        <span className="hx-cx-rule__sub">{rule.summary}</span>
      </span>
      <Switch
        label={rule.title}
        checked={rule.on}
        disabled={busy}
        onChange={(on) => onSet({ rule: rule.id, on })}
      />
      {rule.keywords !== null && (
        <Reveal open={rule.on}>
          <div className="hx-cx-rule__words">
            <label className="hx-conn-form__label" htmlFor={id}>
              {t('next.connectors.ruleWords')}
            </label>
            <input
              id={id}
              className="hx-set__input"
              value={shown}
              disabled={busy}
              spellCheck={false}
              placeholder={t('next.connectors.ruleWordsHint')}
              onChange={(event) => setDraft(event.target.value)}
              onBlur={save}
              onKeyDown={(event) => {
                if (event.key === 'Enter') save();
              }}
            />
          </div>
        </Reveal>
      )}
    </div>
  );
};

/** «What to do with events»: the connection's default rules, loaded when opened. */
const RulesPanel = ({ connId, io }: { connId: string; io: TRulesIO }) => {
  const [rules, setRules] = useState<readonly IConnectorRule[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    void io.load(connId).then((got) => {
      if (!live) return;
      if (typeof got === 'string') setError(got);
      else setRules(got);
    });
    return () => {
      live = false;
    };
  }, [connId, io]);
  const set = async (input: IConnectorRuleInput) => {
    setBusy(input.rule);
    setError(null);
    const got = await io.set(connId, input);
    setBusy(null);
    if (typeof got === 'string') setError(got);
    else setRules(got);
  };
  if (rules === null && error === null) return <Skeleton className="hx-cx-rules__skeleton" />;
  return (
    <div className="hx-cx-rules">
      {(rules ?? []).map((rule) => (
        <RuleLine key={rule.id} rule={rule} busy={busy === rule.id} onSet={(input) => void set(input)} />
      ))}
      {error !== null && <FormError>{error}</FormError>}
    </div>
  );
};

const Reveal = ({ open, children }: { open: boolean; children: ReactNode }) => (
  <div className="hx-reveal" data-open={open}>
    <div className="hx-reveal__in" inert={!open || undefined}>
      {children}
    </div>
  </div>
);

const KindGlyph = ({ kind }: { kind: TConnectorKind }) => (
  <span className="hx-cx-glyph" aria-hidden="true">
    <Icon name={KIND_ICON[kind]} size="sm" />
  </span>
);

interface IRowProps {
  conn: IConnectorConn;
  title: string;
  leaving: boolean;
  onCheck: (id: string) => TDone;
  onRemove: (id: string) => TDone;
  onReissue: ((id: string) => TDone) | null;
  rules: TRulesIO | null;
}

/** A value the person sees once: the address or the secret to give the service, with a copy button. */
const ShownValue = ({ item }: { item: IConnectorShown }) => {
  const id = useId();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(item.value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), CHECKED_MS);
    } catch {
      setCopied(false);
    }
  };
  return (
    <div className="hx-cx-field">
      <label className="hx-conn-form__label" htmlFor={id}>
        {item.label}
      </label>
      <div className="hx-cx-shown">
        <input
          id={id}
          className="hx-set__input hx-cx-shown__value"
          readOnly
          value={item.value}
          spellCheck={false}
          onFocus={(event) => event.target.select()}
        />
        <SetButton tone="ghost" onClick={() => void copy()}>
          {copied ? t('next.connectors.copied') : t('next.connectors.copy')}
        </SetButton>
      </div>
    </div>
  );
};

const ConnectorRow = ({ conn, title, leaving, onCheck, onRemove, onReissue, rules }: IRowProps) => {
  const [rulesOpen, setRulesOpen] = useState(false);
  const [busy, setBusy] = useState<'check' | 'remove' | 'reissue' | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [confirmReissue, setConfirmReissue] = useState(false);
  const [seen, setSeen] = useState<readonly IConnectorShown[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checked, setChecked] = useState(false);

  const check = async () => {
    setBusy('check');
    setError(null);
    const refused = await onCheck(conn.id);
    setBusy(null);
    setError(refused);
    if (refused === null) {
      setChecked(true);
      window.setTimeout(() => setChecked(false), CHECKED_MS);
    }
  };
  const reissue = async () => {
    if (onReissue === null) return;
    setBusy('reissue');
    setError(null);
    const refused = await onReissue(conn.id);
    setBusy(null);
    setError(refused);
    if (refused === null) {
      setConfirmReissue(false);
      setSeen(null);
    }
  };
  const remove = async () => {
    setBusy('remove');
    setError(null);
    const refused = await onRemove(conn.id);
    setBusy(null);
    setError(refused);
  };

  const sub = conn.account !== '' && conn.account !== conn.name ? `${title} · ${conn.account}` : title;
  const problem = conn.state === CONNECTION_STATE.error ? conn.error || t('next.connectors.failed') : null;
  const showing = conn.shown.length > 0 && seen !== conn.shown;
  return (
    <div className="hx-reveal hx-conn-wrap" data-open={!leaving}>
      <div className="hx-reveal__in">
        <div className="hx-conn-row" aria-busy={busy !== null || undefined}>
          <div className="hx-conn-row__head">
            <KindGlyph kind={conn.kind} />
            <span className="hx-conn-row__text">
              <span className="hx-conn-row__name">{conn.name}</span>
              <span className="hx-conn-row__sub">{sub}</span>
              {problem !== null && !checked && (
                <span className="hx-conn-row__state">
                  <Icon name="triangle-alert" size="sm" />
                  {problem}
                </span>
              )}
              {conn.next !== null && problem === null && (
                <span className="hx-cx-row__next">
                  {conn.next.text}
                  {conn.next.url !== '' && (
                    <a className="hx-cx-row__link" href={conn.next.url} target="_blank" rel="noreferrer">
                      {t('next.connectors.openLink')}
                      <Icon name="external-link" size="sm" />
                    </a>
                  )}
                </span>
              )}
              {checked && problem === null && conn.next === null && (
                <span className="hx-conn-row__ok">
                  <Icon name="check" size="sm" />
                  {t('next.connectors.works')}
                </span>
              )}
            </span>
            <span className="hx-cx-row__acts">
              {rules !== null && (
                <SetButton tone="ghost" disabled={leaving} onClick={() => setRulesOpen((open) => !open)}>
                  {t('next.connectors.rules')}
                </SetButton>
              )}
              {onReissue !== null && (
                <SetButton
                  tone="ghost"
                  disabled={busy !== null || leaving}
                  onClick={() => setConfirmReissue(true)}
                >
                  {t('next.connectors.reissue')}
                </SetButton>
              )}
              <SetButton
                tone="ghost"
                busy={busy === 'check'}
                disabled={busy !== null || leaving}
                onClick={() => void check()}
              >
                {t('next.connectors.check')}
              </SetButton>
              <SetButton tone="ghost" disabled={busy !== null || leaving} onClick={() => setConfirm(true)}>
                {t('next.connectors.disconnect')}
              </SetButton>
            </span>
          </div>
          {rules !== null && (
            <Reveal open={rulesOpen && !leaving}>
              <div className="hx-conn-row__panel">
                <p className="hx-conn-row__confirm">{t('next.connectors.rulesLead')}</p>
                {rulesOpen && <RulesPanel connId={conn.id} io={rules} />}
              </div>
            </Reveal>
          )}
          <Reveal open={showing && !leaving}>
            <div className="hx-conn-row__panel">
              <p className="hx-conn-row__confirm">{t('next.connectors.shownLead')}</p>
              {conn.shown.map((item) => (
                <ShownValue key={item.label} item={item} />
              ))}
              <div className="hx-conn-row__buttons">
                <SetButton tone="ghost" onClick={() => setSeen(conn.shown)}>
                  {t('next.connectors.shownDone')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={confirmReissue && !leaving}>
            <div className="hx-conn-row__panel">
              <p className="hx-conn-row__confirm">{t('next.connectors.reissueSure', { name: conn.name })}</p>
              <div className="hx-conn-row__buttons">
                <SetButton tone="danger" busy={busy === 'reissue'} onClick={() => void reissue()}>
                  {t('next.connectors.reissue')}
                </SetButton>
                <SetButton
                  tone="ghost"
                  disabled={busy === 'reissue'}
                  onClick={() => setConfirmReissue(false)}
                >
                  {t('next.connectors.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={confirm && !leaving}>
            <div className="hx-conn-row__panel">
              <p className="hx-conn-row__confirm">
                {t('next.connectors.disconnectSure', { name: conn.name })}
              </p>
              <div className="hx-conn-row__buttons">
                <SetButton tone="danger" busy={busy === 'remove'} onClick={() => void remove()}>
                  {t('next.connectors.disconnect')}
                </SetButton>
                <SetButton tone="ghost" disabled={busy === 'remove'} onClick={() => setConfirm(false)}>
                  {t('next.connectors.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={error !== null && !leaving}>
            <div className="hx-conn-row__err">
              <FormError>{error ?? ''}</FormError>
            </div>
          </Reveal>
        </div>
      </div>
    </div>
  );
};

/** One field of a connector's form, by its type; a choice is a row of buttons, not a native select. */
const FieldInput = ({
  field,
  value,
  disabled,
  found,
  finding,
  canFind,
  onFind,
  onChange,
}: {
  field: IConnectorField;
  value: string;
  disabled: boolean;
  /** The discovered choices, null until asked. */
  found: readonly IChoice[] | null;
  finding: boolean;
  canFind: boolean;
  onFind: () => void;
  onChange: (value: string) => void;
}) => {
  const id = useId();
  const label = field.optional ? t('next.connectors.optional', { label: field.label }) : field.label;
  if (field.type === FIELD_TYPE.choices) {
    const options = field.discover ? (found ?? []) : field.choices;
    const picked = value.split(',').filter((v) => v !== '');
    const toggle = (v: string) =>
      onChange((picked.includes(v) ? picked.filter((x) => x !== v) : [...picked, v]).join(','));
    return (
      <div className="hx-cx-field">
        <span className="hx-conn-form__label" id={id}>
          {label}
        </span>
        {field.discover && found === null && (
          <div className="hx-conn-row__buttons">
            <SetButton tone="ghost" busy={finding} disabled={disabled || !canFind} onClick={onFind}>
              {t('next.connectors.discover')}
            </SetButton>
          </div>
        )}
        <Reveal open={options.length > 0}>
          <div className="hx-cx-choices" role="group" aria-labelledby={id}>
            {options.map((choice) => (
              <button
                key={choice.value}
                type="button"
                aria-pressed={picked.includes(choice.value)}
                className="hx-cx-choice"
                data-on={picked.includes(choice.value) || undefined}
                disabled={disabled}
                onClick={() => toggle(choice.value)}
              >
                {choice.label}
              </button>
            ))}
          </div>
        </Reveal>
        {field.hint !== '' && (found !== null || !field.discover) && (
          <span className="hx-cx-hint">{field.hint}</span>
        )}
      </div>
    );
  }
  if (field.type === FIELD_TYPE.choice)
    return (
      <div className="hx-cx-field">
        <span className="hx-conn-form__label" id={id}>
          {label}
        </span>
        <div className="hx-cx-choices" role="radiogroup" aria-labelledby={id}>
          {field.choices.map((choice) => (
            <button
              key={choice.value}
              type="button"
              role="radio"
              aria-checked={value === choice.value}
              className="hx-cx-choice"
              data-on={value === choice.value || undefined}
              disabled={disabled}
              onClick={() => onChange(choice.value)}
            >
              {choice.label}
            </button>
          ))}
        </div>
        {field.hint !== '' && <span className="hx-cx-hint">{field.hint}</span>}
      </div>
    );
  const inputType: Partial<Record<string, string>> = {
    [FIELD_TYPE.password]: 'password',
    [FIELD_TYPE.email]: 'email',
    [FIELD_TYPE.url]: 'url',
    [FIELD_TYPE.number]: 'text',
  };
  return (
    <div className="hx-cx-field">
      <label className="hx-conn-form__label" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        className="hx-set__input"
        type={field.secret ? 'password' : (inputType[field.type] ?? 'text')}
        inputMode={field.type === FIELD_TYPE.number ? 'numeric' : undefined}
        placeholder={field.placeholder || field.defaultValue}
        value={value}
        autoComplete={field.secret ? 'new-password' : 'off'}
        spellCheck={false}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
      {field.hint !== '' && <span className="hx-cx-hint">{field.hint}</span>}
    </div>
  );
};

interface IFormProps {
  spec: IConnectorSpec;
  open: boolean;
  onConnect: (input: IConnectorInput) => TDone;
  onDiscover?: (input: IDiscoverInput) => Promise<readonly IChoice[] | string>;
  onClose: () => void;
}

const NO_FOUND: Readonly<Record<string, readonly IChoice[]>> = {};

const ConnectorForm = ({ spec, open, onConnect, onDiscover, onClose }: IFormProps) => {
  const [values, setValues] = useState<Readonly<Record<string, string>>>({});
  const [found, setFound] = useState<Readonly<Record<string, readonly IChoice[]>>>(NO_FOUND);
  const [finding, setFinding] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const shown = spec.fields.filter((f) => !f.generate && fieldShown(spec.fields, f, values));
  const empty = (f: IConnectorField) =>
    !f.optional && f.defaultValue === '' && (values[f.id] ?? '').trim() === '';
  const missing = shown.some(empty);
  const shownValues = Object.fromEntries(
    shown.filter((f) => values[f.id] !== undefined).map((f) => [f.id, values[f.id] ?? '']),
  );

  const find = async (field: IConnectorField) => {
    if (onDiscover === undefined || finding !== null) return;
    setFinding(field.id);
    setError(null);
    const got = await onDiscover({ connector: spec.id, field: field.id, values: shownValues });
    setFinding(null);
    if (typeof got === 'string') {
      setError(got);
      return;
    }
    setFound((cur) => ({ ...cur, [field.id]: got }));
  };

  const submit = async () => {
    if (busy || missing) return;
    setBusy(true);
    setError(null);
    const refused = await onConnect({ connector: spec.id, name: '', values: shownValues });
    setBusy(false);
    if (refused !== null) {
      setError(refused);
      return;
    }
    setValues({});
    setFound(NO_FOUND);
    onClose();
  };

  return (
    <Reveal open={open}>
      <form
        className="hx-cx-form"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        {spec.summary !== '' && <p className="hx-cx-hint">{spec.summary}</p>}
        {shown.map((field) => (
          <FieldInput
            key={field.id}
            field={field}
            value={values[field.id] ?? ''}
            disabled={busy}
            found={found[field.id] ?? null}
            finding={finding === field.id}
            canFind={!shown.slice(0, shown.indexOf(field)).some(empty)}
            onFind={() => void find(field)}
            onChange={(value) => {
              // What was found belongs to the values before it: a new login finds again, and nothing stays picked.
              const stale = spec.fields.filter(
                (f) =>
                  f.discover &&
                  spec.fields.indexOf(f) > spec.fields.indexOf(field) &&
                  found[f.id] !== undefined,
              );
              setValues((cur) => ({
                ...cur,
                ...Object.fromEntries(stale.map((f) => [f.id, ''])),
                [field.id]: value,
              }));
              if (stale.length > 0) setFound(NO_FOUND);
              setError(null);
            }}
          />
        ))}
        <Reveal open={error !== null}>
          <FormError>{error ?? ''}</FormError>
        </Reveal>
        <div className="hx-conn-row__buttons">
          <SetButton tone="primary" busy={busy} disabled={missing} onClick={() => void submit()}>
            {t('next.connectors.connect')}
          </SetButton>
          <SetButton tone="ghost" disabled={busy} onClick={onClose}>
            {t('next.connectors.cancel')}
          </SetButton>
        </div>
      </form>
    </Reveal>
  );
};

const RowsSkeleton = () => (
  <Delayed>
    <div className="hx-conn-sk">
      <Skeleton className="hx-conn-sk__row" />
      <Skeleton className="hx-conn-sk__row" />
    </div>
  </Delayed>
);

const ConnectorsSection = ({
  catalog,
  connections,
  failed = null,
  onConnect,
  onDiscover,
  onCheck,
  onRemove,
  onReissue,
  onRules,
  onSetRule,
  ruled = NO_RULED,
}: IConnectorsSectionProps) => {
  const [opened, setOpened] = useState<string | null>(null);
  const rulesIO = useMemo(
    () => (onRules !== undefined && onSetRule !== undefined ? { load: onRules, set: onSetRule } : null),
    [onRules, onSetRule],
  );
  const rows = useLeavingList(connections ?? NO_CONNS, (conn) => conn.id, LEAVE_MS);
  if (catalog === null || connections === null)
    return (
      <Group title={t('next.connectors.title')}>
        {failed === null && <RowsSkeleton />}
        {failed !== null && (
          <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
        )}
      </Group>
    );
  const titleOf = (id: string) => catalog.find((spec) => spec.id === id)?.title ?? id;
  const reissuable = (id: string) =>
    catalog.find((spec) => spec.id === id)?.fields.some((f) => f.generate) === true;
  const kinds = CONNECTOR_KINDS.filter((kind) => catalog.some((spec) => spec.kind === kind));
  return (
    <>
      {rows.length > 0 && (
        <Group title={t('next.connectors.connected')}>
          {rows.map(({ item, leaving }) => (
            <ConnectorRow
              key={item.id}
              conn={item}
              title={titleOf(item.connector)}
              leaving={leaving}
              onCheck={onCheck}
              onRemove={onRemove}
              onReissue={onReissue !== undefined && reissuable(item.connector) ? onReissue : null}
              rules={rulesIO !== null && ruled.includes(item.connector) ? rulesIO : null}
            />
          ))}
        </Group>
      )}
      <Group title={t('next.connectors.title')} foot={t('next.connectors.foot')}>
        {kinds.map((kind) => (
          <div key={kind} className="hx-cx-kind">
            <div className="hx-cx-kind__title">{t(`next.connectors.kind.${kind}`)}</div>
            {catalog
              .filter((spec) => spec.kind === kind)
              .map((spec) => {
                const open = opened === spec.id;
                return (
                  <div key={spec.id} className="hx-cx-card" data-open={open || undefined}>
                    <button
                      type="button"
                      className="hx-cx-card__head"
                      aria-expanded={open}
                      onClick={() => setOpened(open ? null : spec.id)}
                    >
                      <KindGlyph kind={spec.kind} />
                      <span className="hx-cx-card__title">{spec.title}</span>
                      <Icon name="chevron-down" size="sm" className="hx-cx-card__chev" />
                    </button>
                    <ConnectorForm
                      spec={spec}
                      open={open}
                      onConnect={onConnect}
                      onDiscover={onDiscover}
                      onClose={() => setOpened(null)}
                    />
                  </div>
                );
              })}
          </div>
        ))}
      </Group>
    </>
  );
};

export default ConnectorsSection;
