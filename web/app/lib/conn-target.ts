/** A tab target that names a connection: `conn:<connection id>:<issue|page>:<key>` (internal/panel ConnTarget). The
 *  key is the provider's own id, kept as written (case and hyphens); only the first two colons split. */
export interface IConnTarget {
  conn: string;
  kind: 'issue' | 'page';
  key: string;
}

export const parseConnTarget = (target: string | null | undefined): IConnTarget | null => {
  if (typeof target !== 'string' || !target.startsWith('conn:')) return null;
  const rest = target.slice('conn:'.length);
  const first = rest.indexOf(':');
  if (first <= 0) return null;
  const tail = rest.slice(first + 1);
  const second = tail.indexOf(':');
  if (second < 0) return null;
  const kind = tail.slice(0, second);
  const key = tail.slice(second + 1);
  if ((kind !== 'issue' && kind !== 'page') || !key) return null;
  return { conn: rest.slice(0, first), kind, key };
};
