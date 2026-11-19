// «Свой сервис» (Settings → Connections): a service that is in no catalog — the company's internal API, a self-made MCP
// server, a local one. The way in is the agent: the button puts a ready request in a new chat's field and the agent
// finds out what the service is, tries it, describes it and asks for its key through the safe; the person agrees on a
// card. The manual forms are for those who know the address (an MCP server, an API with one key) and for a command of
// a local server. Keys typed here go to the machine's safe and come back nowhere.
import { useId, useState } from 'react';
import type { IMcpAdded, IOwnServiceInput } from '@/next/features/connections/mcp-api';
import { EnvFields, StartingHint } from '@/next/features/connections/mcp-catalog';
import { CustomServer } from '@/next/features/connections/service-list';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import { goTo, ROUTE } from '@/next/routes/routes';
import { NEW_CHAT, setDraftText } from '@/next/stores/drafts';
import type { ReactNode } from 'react';

export type TAddOwn = (
  input: IOwnServiceInput,
  values: Readonly<Record<string, string>>,
) => Promise<IMcpAdded | { error: string }>;

/** The ready request the button puts into a new chat. */
export const askAgentDraft = (): string => t('next.connections.own.draft');

const OWN_KIND = { mcp: 'mcp', api: 'api', local: 'local' } as const;
type TOwnKind = (typeof OWN_KIND)[keyof typeof OWN_KIND];

/** The key's variable of a form-made API. */
export const API_KEY_NAME = 'API_KEY';

/** `Authorization: Bearer {API_KEY}` unless the person says otherwise: the template an API's key goes in. */
export const apiHeaderTemplate = (prefix: string): string => `${prefix}{${API_KEY_NAME}}`;

/** A command typed on one line, cut into words (quotes are not interpreted: a local server's command has none). */
export const commandWords = (line: string): string[] =>
  line
    .trim()
    .split(/\s+/)
    .filter((w) => w !== '');

const Field = ({
  label,
  value,
  onChange,
  placeholder,
  disabled,
  hint,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  disabled: boolean;
  hint?: string;
}) => {
  const id = useId();
  return (
    <div className="hx-conn-form__field">
      <label className="hx-conn-form__label" htmlFor={id}>
        {label}
      </label>
      <input
        id={id}
        className="hx-set__input"
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
      />
      {hint !== undefined && <span className="hx-mcp-env__hint">{hint}</span>}
    </div>
  );
};

interface IFormProps {
  onAdd: TAddOwn;
  onOpen: (item: string) => void;
}

/** Runs the add and, when the machine took it, opens the service's page. */
const useAdd = ({ onAdd, onOpen }: IFormProps) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async (input: IOwnServiceInput, values: Record<string, string>) => {
    if (busy) return;
    setBusy(true);
    setError(null);
    const got = await onAdd(input, values);
    setBusy(false);
    if ('error' in got) {
      setError(got.error);
      return;
    }
    onOpen(got.server.catalogId === '' ? got.server.id : got.server.catalogId);
  };
  return { busy, error, setError, run };
};

/** An API with one key: its base address, where the key goes, and a path that answers once the key is right. */
const ApiForm = (props: IFormProps) => {
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [header, setHeader] = useState('Authorization');
  const [prefix, setPrefix] = useState('Bearer ');
  const [check, setCheck] = useState('');
  const [key, setKey] = useState('');
  const { busy, error, setError, run } = useAdd(props);
  const ready = name.trim() !== '' && /^https?:\/\/\S+$/.test(url.trim()) && header.trim() !== '';
  const fields = [
    {
      name: API_KEY_NAME,
      label: t('next.connections.own.key'),
      secret: true,
      optional: true,
      hint: '',
      default: '',
    },
  ];
  const go = () =>
    void run(
      {
        kind: OWN_KIND.api,
        name: name.trim(),
        url: url.trim(),
        keys: [{ name: API_KEY_NAME, label: t('next.connections.own.key'), optional: true }],
        apiHeaders: { [header.trim()]: apiHeaderTemplate(prefix) },
        ...(check.trim() === '' ? {} : { checkPath: check.trim() }),
      },
      key.trim() === '' ? {} : { [API_KEY_NAME]: key.trim() },
    );
  return (
    <div className="hx-own__form">
      <Field label={t('next.connections.own.name')} value={name} onChange={setName} disabled={busy} />
      <Field
        label={t('next.connections.own.baseUrl')}
        value={url}
        onChange={(v) => {
          setUrl(v);
          setError(null);
        }}
        placeholder="https://api.example.com/v1"
        disabled={busy}
      />
      <div className="hx-mcp-head">
        <Field label={t('next.connections.own.header')} value={header} onChange={setHeader} disabled={busy} />
        <Field label={t('next.connections.own.prefix')} value={prefix} onChange={setPrefix} disabled={busy} />
      </div>
      <Field
        label={t('next.connections.own.check')}
        value={check}
        onChange={setCheck}
        placeholder="/me"
        disabled={busy}
        hint={t('next.connections.own.checkHint')}
      />
      <EnvFields
        fields={fields}
        values={{ [API_KEY_NAME]: key }}
        keep={false}
        disabled={busy}
        invalid={error !== null}
        onChange={(_, value) => {
          setKey(value);
          setError(null);
        }}
        onSubmit={go}
      />
      <p className="hx-svc-note">{t('next.connections.own.apiNote')}</p>
      <div className="hx-svc-form__go">
        <SetButton tone="primary" busy={busy} disabled={!ready} onClick={go}>
          {t('next.connections.connect')}
        </SetButton>
      </div>
      {error !== null && <FormError>{error}</FormError>}
    </div>
  );
};

/** A local server: someone else's code that runs on this machine, so it takes an explicit yes. */
const LocalForm = (props: IFormProps) => {
  const [name, setName] = useState('');
  const [line, setLine] = useState('');
  const [yes, setYes] = useState(false);
  const { busy, error, setError, run } = useAdd(props);
  const words = commandWords(line);
  const ready = name.trim() !== '' && words.length >= 2 && yes;
  return (
    <div className="hx-own__form">
      <Field label={t('next.connections.own.name')} value={name} onChange={setName} disabled={busy} />
      <Field
        label={t('next.connections.own.command')}
        value={line}
        onChange={(v) => {
          setLine(v);
          setError(null);
        }}
        placeholder="npx -y some-mcp-server@1.2.3"
        disabled={busy}
        hint={t('next.connections.own.commandHint')}
      />
      <label className="hx-own__yes">
        <input
          type="checkbox"
          checked={yes}
          disabled={busy}
          onChange={(event) => setYes(event.target.checked)}
        />
        <span>{t('next.connections.own.localYes')}</span>
      </label>
      {busy && <StartingHint />}
      <div className="hx-svc-form__go">
        <SetButton
          tone="primary"
          busy={busy}
          disabled={!ready}
          onClick={() => void run({ kind: OWN_KIND.local, name: name.trim(), command: words }, {})}
        >
          {t('next.connections.own.localGo')}
        </SetButton>
      </div>
      {error !== null && <FormError>{error}</FormError>}
    </div>
  );
};

interface IBlockProps extends IFormProps {
  /** The form of an MCP server by its address (the probe and the sign-in are the page's). */
  mcpForm: ReactNode;
}

/** The block «Свой сервис»: the agent's way first, the manual forms folded under it. */
const OwnServiceBlock = ({ onAdd, onOpen, mcpForm }: IBlockProps) => {
  const [kind, setKind] = useState<TOwnKind>(OWN_KIND.mcp);
  return (
    <section className="hx-cn-block hx-own" aria-labelledby="hx-own-title">
      <h3 className="hx-cn-block__title" id="hx-own-title">
        {t('next.connections.own.title')}
      </h3>
      <p className="hx-own__lead">{t('next.connections.own.lead')}</p>
      <div className="hx-own__acts">
        <SetButton
          tone="primary"
          icon="sparkles"
          onClick={() => {
            setDraftText(NEW_CHAT, askAgentDraft());
            goTo({ kind: ROUTE.home });
          }}
        >
          {t('next.connections.own.withAgent')}
        </SetButton>
      </div>
      <CustomServer title={t('next.connections.own.manual')}>
        <div className="hx-own__kinds" role="group" aria-label={t('next.connections.own.kind')}>
          {Object.values(OWN_KIND).map((k) => (
            <button
              key={k}
              type="button"
              className="hx-own__kind"
              aria-pressed={kind === k}
              onClick={() => setKind(k)}
            >
              {t(`next.connections.own.kind.${k}`)}
            </button>
          ))}
        </div>
        {kind === OWN_KIND.mcp && mcpForm}
        {kind === OWN_KIND.api && <ApiForm onAdd={onAdd} onOpen={onOpen} />}
        {kind === OWN_KIND.local && <LocalForm onAdd={onAdd} onOpen={onOpen} />}
      </CustomServer>
    </section>
  );
};

export default OwnServiceBlock;
