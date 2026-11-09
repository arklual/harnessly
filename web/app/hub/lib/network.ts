// The closed network's policy as the admin edits it (internal/egress.Wire, the hub's /api/admin/network): the form's
// fields as text, the check of what is typed before it is sent, and the answer of the hub's own check put back on fields.

export type Publisher = { name: string; key: string };

export type NetworkPolicy = {
  internalHosts?: string[];
  caPem?: string;
  proxy?: string;
  noProxy?: string[];
  publishers?: Publisher[];
  requireSigned?: boolean;
};

export type NetworkField = 'internalHosts' | 'caPem' | 'proxy' | 'noProxy' | 'publishers' | 'requireSigned';

export type NetworkForm = {
  internalHosts: string;
  caPem: string;
  proxy: string;
  noProxy: string;
  publishers: Publisher[];
  requireSigned: boolean;
};

/** A field's problem: the text is a key of the hub's dictionary, or the hub's own words when it found the problem. */
export type NetworkProblem = { field: NetworkField; key?: string; text?: string; value?: string };

const HOST = /^(\*\.)?[a-z0-9]([a-z0-9._-]*[a-z0-9])?$/;
const NO_PROXY = /^(\*\.|\.)?[a-z0-9]([a-z0-9._-]*[a-z0-9])?$/;
const CIDR = /^[0-9a-f:.]+\/\d{1,3}$/;
const ADDRESS = /^([0-9]{1,3}\.){3}[0-9]{1,3}$|^[0-9a-f]*:[0-9a-f:]+$/;

/** Lines, commas or semicolons separate entries; empty ones and repeats go. */
export const splitList = (text: string): string[] => {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split(/[\n,;]+/)) {
    const v = raw.trim().toLowerCase();
    if (v && !seen.has(v)) {
      seen.add(v);
      out.push(v);
    }
  }
  return out;
};

export const emptyForm = (): NetworkForm => ({
  internalHosts: '',
  caPem: '',
  proxy: '',
  noProxy: '',
  publishers: [],
  requireSigned: false,
});

export const toForm = (p: NetworkPolicy): NetworkForm => ({
  internalHosts: (p.internalHosts ?? []).join('\n'),
  caPem: p.caPem ?? '',
  proxy: p.proxy ?? '',
  noProxy: (p.noProxy ?? []).join('\n'),
  publishers: (p.publishers ?? []).map((x) => ({ ...x })),
  requireSigned: p.requireSigned === true,
});

export const toPolicy = (f: NetworkForm): NetworkPolicy => {
  const out: NetworkPolicy = {};
  const hosts = splitList(f.internalHosts);
  if (hosts.length > 0) out.internalHosts = hosts;
  if (f.caPem.trim()) out.caPem = f.caPem.trim() + '\n';
  if (f.proxy.trim()) out.proxy = f.proxy.trim();
  const noProxy = splitList(f.noProxy);
  if (noProxy.length > 0) out.noProxy = noProxy;
  const publishers = f.publishers
    .map((x) => ({ name: x.name.trim(), key: x.key.trim() }))
    .filter((x) => x.name !== '' || x.key !== '');
  if (publishers.length > 0) out.publishers = publishers;
  if (f.requireSigned) out.requireSigned = true;
  return out;
};

const keyBytes = (key: string): number | null => {
  try {
    return atob(key).length;
  } catch {
    return null;
  }
};

const validProxy = (raw: string): boolean => {
  try {
    const u = new URL(raw.includes('://') ? raw : `http://${raw}`);
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.hostname !== '';
  } catch {
    return false;
  }
};

/** What is wrong with the form, field by field; the hub checks the same again and its words win. */
export const checkForm = (f: NetworkForm): NetworkProblem[] => {
  const ps: NetworkProblem[] = [];
  for (const h of splitList(f.internalHosts)) {
    if (!HOST.test(h)) ps.push({ field: 'internalHosts', key: 'network.err.host', value: h });
  }
  const pem = f.caPem.trim();
  if (pem) {
    if (pem.includes('PRIVATE KEY')) ps.push({ field: 'caPem', key: 'network.err.caKey' });
    else if (!pem.includes('-----BEGIN CERTIFICATE-----'))
      ps.push({ field: 'caPem', key: 'network.err.caPem' });
  }
  if (f.proxy.trim() && !validProxy(f.proxy.trim())) ps.push({ field: 'proxy', key: 'network.err.proxy' });
  for (const e of splitList(f.noProxy)) {
    if (e !== '*' && !NO_PROXY.test(e) && !CIDR.test(e) && !ADDRESS.test(e)) {
      ps.push({ field: 'noProxy', key: 'network.err.noProxy', value: e });
    }
  }
  const keys = new Set<string>();
  for (const p of f.publishers) {
    const name = p.name.trim();
    const key = p.key.trim();
    if (name === '' && key === '') continue;
    if (name === '') ps.push({ field: 'publishers', key: 'network.err.pubName' });
    else if (keyBytes(key) !== 32) ps.push({ field: 'publishers', key: 'network.err.pubKey', value: name });
    else if (keys.has(key)) ps.push({ field: 'publishers', key: 'network.err.pubTwice', value: name });
    else keys.add(key);
  }
  if (f.requireSigned && keys.size === 0)
    ps.push({ field: 'requireSigned', key: 'network.err.noPublishers' });
  return ps;
};

const FIELDS: readonly string[] = [
  'internalHosts',
  'caPem',
  'proxy',
  'noProxy',
  'publishers',
  'requireSigned',
];

/** The hub's 422 (`details.problems`) as problems of fields. */
export const problemsOf = (details: Record<string, unknown>): NetworkProblem[] => {
  const raw = details.problems;
  if (!Array.isArray(raw)) return [];
  const out: NetworkProblem[] = [];
  for (const p of raw as unknown[]) {
    if (p && typeof p === 'object') {
      const { field, message } = p as { field?: unknown; message?: unknown };
      if (typeof field === 'string' && FIELDS.includes(field) && typeof message === 'string') {
        out.push({ field: field as NetworkField, text: message });
      }
    }
  }
  return out;
};
