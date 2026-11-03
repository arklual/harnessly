// What the Connections page says about a product, a sign-in method, a field and a refusal, in the person's words.
import {
  CONN_REASONS,
  CONN_STATE,
  DEPLOYMENT,
  type IConn,
  type IDetection,
  type IMethod,
} from '@/next/features/connections/api';
import { t } from '@/next/i18n';

/** The products the runtime knows (connections.Kind); each has its name and its mark. */
export const KINDS = [
  'jira',
  'confluence',
  'youtrack',
  'linear',
  'gitlab',
  'github',
  'gitea',
  'bitbucket',
  'redmine',
  'yandex-tracker',
  'azure-devops',
  'notion',
  'mediawiki',
  'outline',
  'bookstack',
  'wikijs',
] as const;
export type TKind = (typeof KINDS)[number];

export const isKnownKind = (kind: string): kind is TKind => KINDS.some((known) => known === kind);

/** The fields the page has a label for (connections.Field*). */
const FIELDS = ['token', 'email', 'username', 'password', 'org', 'tokenId'] as const;
/** The sign-in methods the page has a name for (connections.Method.ID). */
const METHODS = ['api-token', 'pat', 'basic', 'api-key', 'oauth-token', 'none'] as const;

const known = (list: readonly string[], id: string): boolean => list.some((item) => item === id);

/** «Jira», «Yandex Tracker»; a product the page does not know yet by the runtime's own word. */
export const kindName = (kind: string): string =>
  isKnownKind(kind) ? t(`next.connections.kind.${kind}`) : kind;

export const fieldLabel = (id: string): string =>
  known(FIELDS, id) ? t(`next.connections.field.${id}`) : id;

export const methodLabel = (id: string): string =>
  known(METHODS, id) ? t(`next.connections.method.${id}`) : id;

export const deploymentText = (deployment: string): string =>
  deployment === DEPLOYMENT.cloud ? t('next.connections.cloud') : t('next.connections.selfHosted');

/** «jira.acme.com/jira»: the address without its scheme. */
export const hostOf = (baseUrl: string): string => baseUrl.replace(/^[a-z]+:\/\//i, '').replace(/\/$/, '');

/** What a refusal of the runtime says to the person. */
export const reasonText = (code: string): string =>
  known(CONN_REASONS, code)
    ? t(`next.connections.reason.${code}`)
    : t('next.connections.reason.other', { code });

/** What a connection's last check found, when it is not fine; null when it is (nothing to say then). */
export const stateText = (conn: IConn): string | null => {
  if (conn.state === CONN_STATE.ok) return null;
  if (conn.state === CONN_STATE.denied) return t('next.connections.state.denied');
  return t('next.connections.state.unreachable');
};

/** The method the page preselects: the recommended one, else the first. */
export const firstMethod = (detection: IDetection): IMethod | null =>
  detection.methods.find((method) => method.recommended) ?? detection.methods[0] ?? null;

/** Another connection can do something this one does: then which of them is the primary matters. */
export const sharesWork = (conn: IConn, all: readonly IConn[]): boolean =>
  all.some(
    (other) => other.id !== conn.id && other.capabilities.some((cap) => conn.capabilities.includes(cap)),
  );

/** The list after `made` became the primary: no other connection of its capabilities is (as the runtime does). */
export const withPrimary = (all: readonly IConn[], made: IConn): IConn[] =>
  all.map((conn) => {
    if (conn.id === made.id) return made;
    if (!conn.capabilities.some((cap) => made.capabilities.includes(cap))) return conn;
    return { ...conn, primary: false };
  });

/** The list with `conn` in it: in its old place when it was there (a new sign-in of the same account), else last. */
export const withConn = (all: readonly IConn[], conn: IConn): IConn[] =>
  all.some((item) => item.id === conn.id)
    ? all.map((item) => (item.id === conn.id ? conn : item))
    : [...all, conn];
