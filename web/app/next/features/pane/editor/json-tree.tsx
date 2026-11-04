// A JSON file as a tree (.hx-jtree): objects and arrays fold, the first two levels open; a long array shows its first
// items and how many more there are. Text that is not JSON says where it breaks; the code is a click away.
import { useState } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

type TJson = null | boolean | number | string | TJson[] | { [key: string]: TJson };

const SHOWN = 200;
const OPEN_DEPTH = 2;

const kindOf = (v: TJson) => {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'array';
  return typeof v;
};

const Leaf = ({ value }: { value: TJson }) => {
  const kind = kindOf(value);
  const text = kind === 'string' ? JSON.stringify(value) : String(value);
  return (
    <span className="hx-jtree__val" data-kind={kind}>
      {text}
    </span>
  );
};

const Node = ({ name, value, depth }: { name: string | null; value: TJson; depth: number }) => {
  const [open, setOpen] = useState(depth < OPEN_DEPTH);
  const [all, setAll] = useState(false);
  const branch = value !== null && typeof value === 'object';
  const label = name !== null && <span className="hx-jtree__key">{name}</span>;
  if (!branch)
    return (
      <div className="hx-jtree__row">
        <span className="hx-jtree__pad" />
        {label}
        {label && <span className="hx-jtree__colon">:</span>}
        <Leaf value={value} />
      </div>
    );
  const entries: [string, TJson][] = Array.isArray(value)
    ? value.map((v, i) => [String(i), v])
    : Object.entries(value);
  const shown = all ? entries : entries.slice(0, SHOWN);
  const summary = Array.isArray(value)
    ? t('next.jtree.items', { n: entries.length })
    : t('next.jtree.keys', { n: entries.length });
  return (
    <div className="hx-jtree__node">
      <button type="button" className="hx-jtree__row" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <span className="hx-jtree__fold" data-open={open || undefined}>
          <Icon name="chevron-right" size="sm" />
        </span>
        {label}
        {label && <span className="hx-jtree__colon">:</span>}
        <span className="hx-jtree__brace">{Array.isArray(value) ? '[ ]' : '{ }'}</span>
        <span className="hx-jtree__sum">{summary}</span>
      </button>
      {open && (
        <div className="hx-jtree__kids">
          {shown.map(([k, v]) => (
            <Node key={k} name={k} value={v} depth={depth + 1} />
          ))}
          {!all && entries.length > SHOWN && (
            <button type="button" className="hx-jtree__more" onClick={() => setAll(true)}>
              {t('next.jtree.more', { n: entries.length - SHOWN })}
            </button>
          )}
        </div>
      )}
    </div>
  );
};

const parse = (text: string): { ok: true; value: TJson } | { ok: false; error: string } => {
  try {
    return { ok: true, value: JSON.parse(text) as TJson };
  } catch (failure) {
    return { ok: false, error: failure instanceof Error ? failure.message : String(failure) };
  }
};

const JsonTree = ({ text }: { text: string }) => {
  const parsed = parse(text);
  if (!parsed.ok)
    return (
      <div className="hx-jtree__bad" role="alert">
        <Icon name="triangle-alert" size="sm" />
        <span>{t('next.jtree.bad', { error: parsed.error })}</span>
      </div>
    );
  return (
    <div className="hx-jtree">
      <Node name={null} value={parsed.value} depth={0} />
    </div>
  );
};

export default JsonTree;
