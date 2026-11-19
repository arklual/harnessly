// hl.connections on the page's side: what the runtime answers a view's call of the person's connections, and
// the two questions only the person answers. The runtime decides (cmd/runtime/routes_apps_connections.go): a change
// comes back as `needsConfirm` and goes out only after the person's yes to that very call; a connection the app has no
// agreement for comes back as `needsConsent` when the view asks for it with hl.connections.request. The page shows the
// question in its own dialog, titled with the app's real name, and repeats the call.
import { BridgeError } from '@/next/features/app-views/bridge';

export type TConnectionOp = 'list' | 'tools' | 'call' | 'api' | 'request';

/** A change the view asked for: which connection, what, with which arguments (as the person reads them). */
export interface IConnectionConfirm {
  connection: string;
  name: string;
  what: string;
  detail?: string;
}

/** The agreement the view asked for: the app wants this connection, as far as `access` (read or all). */
export interface IConnectionConsent {
  connection: string;
  name: string;
  access: 'read' | 'all';
  reason?: string;
}

export interface IConnectionIO {
  /** One call of the runtime's route (POST <platform>/apps/connections) with the view's session. */
  call: (body: Record<string, unknown>) => Promise<Record<string, unknown>>;
  /** The person's answer to a question the host asks in its own dialog; true only for «ОК». */
  ask: (
    question: { kind: 'consent'; card: IConnectionConsent } | { kind: 'confirm'; change: IConnectionConfirm },
  ) => Promise<boolean>;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

/** What the person is asked about a change, in one text: the connection, then what goes out. */
export const confirmText = (
  c: IConnectionConfirm,
  t: (key: string, vars?: Record<string, string>) => string,
): string => t('next.appview.connectConfirm', { name: c.name, what: c.what, detail: c.detail ?? '' }).trim();

export const consentText = (
  c: IConnectionConsent,
  t: (key: string, vars?: Record<string, string>) => string,
): string =>
  t(c.access === 'all' ? 'next.appview.connectConsentAll' : 'next.appview.connectConsentRead', {
    name: c.name,
    reason: c.reason ?? '',
  }).trim();

/**
 * Runs one op of hl.connections. The answer of list is `{connections}`, of tools `{tools}`, of call the tool's own MCP
 * result, of api `{status, contentType, body}`, of request `{granted, access}`.
 */
export const runConnection = async (
  op: TConnectionOp,
  params: Record<string, unknown>,
  io: IConnectionIO,
): Promise<unknown> => {
  const out = await io.call({ ...params, op });
  if (op === 'request') {
    const card = out.needsConsent;
    if (!isRecord(card)) return out;
    const consent = card as unknown as IConnectionConsent;
    if (!(await io.ask({ kind: 'consent', card: consent }))) return { granted: false, access: '' };
    // The person's yes goes to the runtime as the page's own word: a view has no such method.
    return io.call({ op: 'grant', connection: params.connection, access: consent.access });
  }
  if (op !== 'call' && op !== 'api') return out;
  const change = out.needsConfirm;
  if (!isRecord(change)) return op === 'call' ? out.result : out;
  if (!(await io.ask({ kind: 'confirm', change: change as unknown as IConnectionConfirm })))
    throw new BridgeError('permission_denied', 'the person did not allow it');
  const done = await io.call({ ...params, op, confirmed: true });
  if (isRecord(done.needsConfirm)) throw new BridgeError('permission_denied', 'the person did not allow it');
  return op === 'call' ? done.result : done;
};
