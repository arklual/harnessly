// The person's connectors (internal/connectors, docs/connectors.md; the runtime's <platform>/connectors routes): the
// catalog drawn from each connector's description, the connections, and the actions outward the agents propose, which
// run only on the person's yes. A secret goes one way: the page sends it with the connect and never gets one back.
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { list, obj, optStr, str } from '@/next/features/connections/api';
import { locale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** Connecting and checking ask the service: a slow one gets more time. */
const CHECK_TIMEOUT_MS = 50_000;
/** The card's wait for a change of the proposals, a little over the runtime's 25 s. */
const WATCH_TIMEOUT_MS = 30_000;

export const CONNECTOR_KIND = {
  mail: 'mail',
  calendar: 'calendar',
  chat: 'chat',
  files: 'files',
  feed: 'feed',
  webhook: 'webhook',
} as const;
export type TConnectorKind = (typeof CONNECTOR_KIND)[keyof typeof CONNECTOR_KIND];
export const CONNECTOR_KINDS: readonly TConnectorKind[] = Object.values(CONNECTOR_KIND);

export const FIELD_TYPE = {
  text: 'text',
  url: 'url',
  email: 'email',
  number: 'number',
  path: 'path',
  password: 'password',
  choice: 'choice',
  choices: 'choices',
} as const;
export type TFieldType = (typeof FIELD_TYPE)[keyof typeof FIELD_TYPE];

export interface IChoice {
  value: string;
  label: string;
}

/** A field shown only while another (a choice) has one of these values. */
export interface IFieldWhen {
  field: string;
  values: readonly string[];
}

export interface IConnectorField {
  id: string;
  type: TFieldType;
  label: string;
  hint: string;
  placeholder: string;
  optional: boolean;
  defaultValue: string;
  choices: readonly IChoice[];
  secret: boolean;
  when: IFieldWhen | null;
  /** The choices come from the service: the form asks for them with the values filled in so far. */
  discover: boolean;
  /** A secret the machine makes itself (a webhook's key): the form does not ask it. */
  generate: boolean;
}

export interface IConnectorSpec {
  id: string;
  kind: TConnectorKind;
  title: string;
  summary: string;
  fields: readonly IConnectorField[];
}

export const CONNECTION_STATE = { ok: 'ok', error: 'error' } as const;

export interface IConnectorConn {
  id: string;
  connector: string;
  kind: TConnectorKind;
  name: string;
  account: string;
  state: string;
  error: string;
  /** The step the person still has to do (a Telegram bot's pairing link), null when there is none. */
  next: IConnectorStep | null;
  /** What the person sees once, right after a connect or a reissue: the address, a generated secret. */
  shown: readonly IConnectorShown[];
}

export interface IConnectorStep {
  text: string;
  url: string;
}

export interface IConnectorShown {
  label: string;
  value: string;
}

export interface IConnectorPair {
  name: string;
  value: string;
}

export const PROPOSAL_STATE = {
  waiting: 'waiting',
  done: 'done',
  failed: 'failed',
  declined: 'declined',
  expired: 'expired',
} as const;

export interface IConnectorProposal {
  id: string;
  connection: string;
  name: string;
  title: string;
  args: readonly IConnectorPair[];
  why: string;
  conv: string;
  state: string;
  result: string;
}

export interface IConnectorRefusal {
  refused: string;
  message: string;
}

export type TConnectorAnswer<T> = T | IConnectorRefusal;
export type TConnectorOutcome<T> = PageOutcome<TConnectorAnswer<T>>;

export const isConnectorRefusal = <T>(v: TConnectorAnswer<T>): v is IConnectorRefusal =>
  typeof v === 'object' && v !== null && 'refused' in v;

/** A word of the runtime in the page's language: {en, ru}, Russian falling back to English. */
const word = (v: unknown, what: string): string => {
  if (v === undefined || v === null) return '';
  const o = obj(v, what);
  const en = optStr(o.en, `${what}.en`);
  const ru = optStr(o.ru, `${what}.ru`);
  return locale() === 'ru' && ru !== '' ? ru : en;
};

const kindOf = (v: unknown): TConnectorKind => {
  const found = CONNECTOR_KINDS.find((k) => k === v);
  if (found === undefined) throw new Error('connectors: kind');
  return found;
};

const typeOf = (v: unknown): TFieldType => {
  const found = Object.values(FIELD_TYPE).find((k) => k === v);
  return found ?? FIELD_TYPE.text;
};

const readField = (v: unknown): IConnectorField => {
  const o = obj(v, 'field');
  return {
    id: str(o.id, 'field.id'),
    type: typeOf(o.type),
    label: word(o.label, 'field.label'),
    hint: word(o.hint, 'field.hint'),
    placeholder: optStr(o.placeholder, 'field.placeholder'),
    optional: o.optional === true,
    defaultValue: optStr(o.default, 'field.default'),
    choices: list(o.choices, 'field.choices', (c) => {
      const co = obj(c, 'choice');
      return { value: str(co.value, 'choice.value'), label: word(co.label, 'choice.label') };
    }),
    secret: o.secret === true,
    when:
      o.when === undefined || o.when === null
        ? null
        : (() => {
            const w = obj(o.when, 'field.when');
            return {
              field: str(w.field, 'when.field'),
              values: list(w.values, 'when.values', (x) => str(x, 'when.value')),
            };
          })(),
    discover: o.discover === true,
    generate: o.generate === true,
  };
};

/** Whether a field is asked with these values: a field with when only while its choice has one of the values. */
export const fieldShown = (
  fields: readonly IConnectorField[],
  field: IConnectorField,
  values: Readonly<Record<string, string>>,
): boolean => {
  if (field.when === null) return true;
  const { field: other, values: allowed } = field.when;
  const set = (values[other] ?? '').trim();
  const value = set !== '' ? set : (fields.find((f) => f.id === other)?.defaultValue ?? '');
  return allowed.includes(value);
};

export const readSpec = (v: unknown): IConnectorSpec => {
  const o = obj(v, 'spec');
  return {
    id: str(o.id, 'spec.id'),
    kind: kindOf(o.kind),
    title: word(o.title, 'spec.title'),
    summary: word(o.summary, 'spec.summary'),
    fields: list(o.fields, 'spec.fields', readField),
  };
};

export const readConnectorConn = (v: unknown): IConnectorConn => {
  const o = obj(v, 'connection');
  return {
    id: str(o.id, 'id'),
    connector: str(o.connector, 'connector'),
    kind: kindOf(o.kind),
    name: str(o.name, 'name'),
    account: optStr(o.account, 'account'),
    state: str(o.state, 'state'),
    error: optStr(o.error, 'error'),
    next: readStep(o.next),
    shown: list(o.shown, 'shown', (v) => {
      const so = obj(v, 'shown');
      return { label: word(so.label, 'shown.label'), value: str(so.value, 'shown.value') };
    }),
  };
};

const readStep = (v: unknown): IConnectorStep | null => {
  if (v === undefined || v === null) return null;
  const o = obj(v, 'next');
  return { text: word(o.text, 'next.text'), url: optStr(o.url, 'next.url') };
};

export const readProposal = (v: unknown): IConnectorProposal => {
  const o = obj(v, 'proposal');
  return {
    id: str(o.id, 'id'),
    connection: str(o.connection, 'connection'),
    name: optStr(o.name, 'name'),
    title: word(o.title, 'title'),
    args: list(o.args, 'args', (a) => {
      const ao = obj(a, 'arg');
      return { name: str(ao.name, 'arg.name'), value: optStr(ao.value, 'arg.value') };
    }),
    why: optStr(o.why, 'why'),
    conv: optStr(o.conv, 'conv'),
    state: str(o.state, 'state'),
    result: optStr(o.result, 'result'),
  };
};

const orRefusal =
  <T>(read: (o: Record<string, unknown>) => T) =>
  (v: unknown): TConnectorAnswer<T> => {
    const o = obj(v, 'answer');
    if (typeof o.error === 'string') return { refused: o.error, message: optStr(o.message, 'message') };
    return read(o);
  };

export interface IConnectorsList {
  catalog: readonly IConnectorSpec[];
  connections: readonly IConnectorConn[];
  /** The connectors that have default rules (internal/connectors/rules). */
  ruled: readonly string[];
}

const readListing = orRefusal((o): IConnectorsList => ({
  catalog: list(o.catalog, 'catalog', readSpec),
  connections: list(o.connections, 'connections', readConnectorConn),
  ruled: o.ruled === undefined ? [] : list(o.ruled, 'ruled', (v) => str(v, 'ruled')),
}));
const readOne = orRefusal((o) => readConnectorConn(o.connection));
const readDone = orRefusal(() => true as const);
const readChoices = orRefusal((o) =>
  list(o.choices, 'choices', (c): IChoice => {
    const co = obj(c, 'choice');
    return { value: str(co.value, 'choice.value'), label: word(co.label, 'choice.label') };
  }),
);
const readProposals = orRefusal((o) => ({
  proposals: list(o.proposals, 'proposals', readProposal),
  rev: optStr(o.rev, 'rev'),
}));
const readAnswered = orRefusal((o) => readProposal(o.proposal));

const path = (machine: MachineId, rest: `/${string}`) => runtimePath(platformPath(rest), machine);
const one = (id: string, rest = ''): `/${string}` => `/connectors/${encodeURIComponent(id)}${rest}`;

export const loadConnectors = (machine: MachineId) =>
  getPage(path(machine, '/connectors'), readListing, TIMEOUT_MS);

export interface IConnectorInput {
  connector: string;
  name: string;
  values: Readonly<Record<string, string>>;
}

/** Checks the values against the service and keeps the connection; nothing is kept when the check fails. */
export const connectConnector = (machine: MachineId, input: IConnectorInput) =>
  sendBody('POST', path(machine, '/connectors'), input, readOne, CHECK_TIMEOUT_MS);

export interface IDiscoverInput {
  connector: string;
  field: string;
  values: Readonly<Record<string, string>>;
}

/** The choices of a discovered field (an account's calendars) from the values filled in so far; nothing is kept. */
export const discoverConnector = (machine: MachineId, input: IDiscoverInput) =>
  sendBody('POST', path(machine, '/connectors/discover'), input, readChoices, CHECK_TIMEOUT_MS);

export const checkConnector = (machine: MachineId, id: string) =>
  sendPage('POST', path(machine, one(id, '/check')), readOne, CHECK_TIMEOUT_MS);

/** New values of the connection's generated secrets: the old ones stop working at once, the new ones are shown once. */
export const reissueConnector = (machine: MachineId, id: string) =>
  sendPage('POST', path(machine, one(id, '/reissue')), readOne, TIMEOUT_MS);

export const removeConnector = (machine: MachineId, id: string) =>
  sendPage('DELETE', path(machine, one(id)), readDone, TIMEOUT_MS);

/** The proposals waiting for the person; with rev, the answer waits until they move from it. */
export const loadConnectorProposals = (machine: MachineId, rev: string) =>
  getPage(
    path(machine, rev === '' ? '/connector-actions' : `/connector-actions?rev=${encodeURIComponent(rev)}`),
    readProposals,
    rev === '' ? TIMEOUT_MS : WATCH_TIMEOUT_MS,
  );

export const ANSWER_VERB = { approve: 'approve', decline: 'decline' } as const;
export type TAnswerVerb = (typeof ANSWER_VERB)[keyof typeof ANSWER_VERB];

/** The person's yes or no: a yes runs the action exactly as shown. */
export const answerConnectorProposal = (machine: MachineId, id: string, verb: TAnswerVerb) =>
  sendPage(
    'POST',
    path(machine, `/connector-actions/${encodeURIComponent(id)}/${verb}`),
    readAnswered,
    CHECK_TIMEOUT_MS,
  );

/** A default rule of a connection (internal/connectors/rules): what to do with its events, one switch. */
export interface IConnectorRule {
  id: string;
  title: string;
  summary: string;
  on: boolean;
  /** The words the person can change; null for a rule without them. */
  keywords: readonly string[] | null;
}

const readRule = (v: unknown): IConnectorRule => {
  const o = obj(v, 'rule');
  return {
    id: str(o.id, 'rule.id'),
    title: word(o.title, 'rule.title'),
    summary: word(o.summary, 'rule.summary'),
    on: o.on === true,
    keywords:
      o.keywords === undefined || o.keywords === null
        ? null
        : list(o.keywords, 'rule.keywords', (k) => str(k, 'rule.keyword')),
  };
};
const readRules = orRefusal((o) => list(o.rules, 'rules', readRule));

export const loadConnectorRules = (machine: MachineId, id: string) =>
  getPage(path(machine, one(id, '/rules')), readRules, TIMEOUT_MS);

export interface IConnectorRuleInput {
  rule: string;
  on?: boolean;
  keywords?: readonly string[];
}

/** One switch or the words of one rule; the answer is every rule of the connection as it is now. */
export const setConnectorRule = (machine: MachineId, id: string, input: IConnectorRuleInput) =>
  sendBody('PUT', path(machine, one(id, '/rules')), input, readRules, TIMEOUT_MS);
