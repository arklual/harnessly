// The kit's tree on the left: Основа, Компоненты, Шаблоны (template → sections → layouts), Ассеты. Only the first level
// is open at the start; a long branch is drawn by the rows in view only (fixed-height rows). A click selects the node
// and the canvas flies to it; a node selected on the canvas opens its branch here and scrolls into view.
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import type { IKitCanvas } from '@/next/features/designly/kit/kit-api';
import type { TSectionTitles } from '@/next/features/designly/kit/kit-world';
import Icon from '@/next/ui/icon';

export interface ITreeNode {
  node: string;
  label: string;
  /** Shown for a big branch only. */
  count?: number;
  children?: ITreeNode[];
}

export interface ITreeRow {
  node: string;
  label: string;
  depth: number;
  count?: number;
  branch: boolean;
  open: boolean;
}

const BIG = 12;
const ROW_PX = 30;
const OVERSCAN = 8;

const counted = (n: number) => (n >= BIG ? { count: n } : {});

/** The tree of a canvas: its sections with their groups and items. */
export const treeOf = (c: IKitCanvas, titles: TSectionTitles): ITreeNode[] => {
  const named = (id: string) => c.sections.find((s) => s.id === id)?.title || titles[id] || id;
  const out: ITreeNode[] = [];
  for (const s of c.sections) {
    if (s.id === 'foundation') {
      const children: ITreeNode[] = [];
      const colors = c.colors ?? [];
      if (colors.length > 0)
        children.push({
          node: 'group:colors',
          label: titles.colors ?? 'colors',
          ...counted(colors.length),
          children: colors.map((x) => ({ node: x.id, label: x.name })),
        });
      const type = c.type ?? [];
      if (type.length > 0)
        children.push({
          node: 'group:type',
          label: titles.type ?? 'type',
          ...counted(type.length),
          children: type.map((x) => ({ node: x.id, label: `${x.group} · ${x.name}` })),
        });
      const tokens = c.tokens ?? [];
      if (tokens.length > 0)
        children.push({
          node: 'group:tokens',
          label: titles.tokens ?? 'tokens',
          ...counted(tokens.length),
          children: tokens.map((x) => ({ node: x.id, label: x.name })),
        });
      out.push({ node: 'section:foundation', label: named('foundation'), children });
    } else if (s.id === 'components') {
      out.push({
        node: 'section:components',
        label: named('components'),
        children: (c.groups ?? []).map((g) => ({
          node: `cgroup:${g.id}`,
          label: g.title,
          children: g.components.map((k) => ({ node: k.node, label: k.name })),
        })),
      });
    } else if (s.id === 'templates') {
      out.push({
        node: 'section:templates',
        label: named('templates'),
        children: (c.templates ?? []).map((t) => ({
          node: t.node,
          label: t.title,
          ...counted(t.sections.reduce((n, x) => n + x.layouts.length, 0)),
          children: t.sections.map((x) => ({
            node: x.node,
            label: x.title,
            ...counted(x.layouts.length),
            children: x.layouts.map((l) => ({ node: l.node, label: l.name })),
          })),
        })),
      });
    } else if (s.id === 'assets') {
      out.push({
        node: 'section:assets',
        label: named('assets'),
        children: (c.assets ?? []).map((g) => ({
          node: g.node,
          label: g.title,
          ...counted(g.items.length),
          children: g.items.map((a) => ({ node: a.node, label: a.name })),
        })),
      });
    }
  }
  return out;
};

/** The rows the open branches show, in order. */
export const visibleRows = (tree: readonly ITreeNode[], open: ReadonlySet<string>): ITreeRow[] => {
  const rows: ITreeRow[] = [];
  const walk = (nodes: readonly ITreeNode[], depth: number) => {
    for (const n of nodes) {
      const branch = (n.children?.length ?? 0) > 0;
      const isOpen = branch && open.has(n.node);
      rows.push({
        node: n.node,
        label: n.label,
        depth,
        branch,
        open: isOpen,
        ...(n.count ? { count: n.count } : {}),
      });
      if (isOpen && n.children) walk(n.children, depth + 1);
    }
  };
  walk(tree, 0);
  return rows;
};

/** The branches that lead to a node (to open them when it is selected elsewhere). */
export const pathTo = (tree: readonly ITreeNode[], node: string): string[] | null => {
  for (const n of tree) {
    if (n.node === node) return [];
    if (n.children) {
      const below = pathTo(n.children, node);
      if (below) return [n.node, ...below];
    }
  }
  return null;
};

interface IKitTreeProps {
  tree: ITreeNode[];
  selected: string | null;
  onSelect: (node: string) => void;
  label: string;
}

const KitTree = ({ tree, selected, onSelect, label }: IKitTreeProps) => {
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set(tree.map((n) => n.node)));
  const listRef = useRef<HTMLDivElement | null>(null);
  const [scroll, setScroll] = useState({ top: 0, h: 600 });

  // A node selected elsewhere opens its branches (state adjusted during render when the selection changes).
  const [openedFor, setOpenedFor] = useState<string | null>(null);
  if (selected !== openedFor) {
    setOpenedFor(selected);
    const path = selected ? pathTo(tree, selected) : null;
    if (path && !path.every((p) => open.has(p))) setOpen(new Set([...open, ...path]));
  }

  const rows = useMemo(() => visibleRows(tree, open), [tree, open]);

  // …and scrolls into view.
  useEffect(() => {
    const el = listRef.current;
    if (!el || !selected) return;
    const i = rows.findIndex((r) => r.node === selected);
    if (i < 0) return;
    const top = i * ROW_PX;
    if (top < el.scrollTop || top + ROW_PX > el.scrollTop + el.clientHeight)
      el.scrollTo({ top: Math.max(0, top - el.clientHeight / 3) });
  }, [selected, rows]);

  useEffect(() => {
    const el = listRef.current;
    if (!el) return;
    const read = () => setScroll({ top: el.scrollTop, h: el.clientHeight });
    const ro = new ResizeObserver(read);
    ro.observe(el);
    el.addEventListener('scroll', read, { passive: true });
    read();
    return () => {
      ro.disconnect();
      el.removeEventListener('scroll', read);
    };
  }, []);

  const toggle = (node: string) =>
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(node)) next.delete(node);
      else next.add(node);
      return next;
    });

  const from = Math.max(0, Math.floor(scroll.top / ROW_PX) - OVERSCAN);
  const to = Math.min(rows.length, Math.ceil((scroll.top + scroll.h) / ROW_PX) + OVERSCAN);

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = rows.findIndex((r) => r.node === selected);
    const row = rows[i];
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const next = rows[Math.max(0, Math.min(rows.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)))];
      if (next) onSelect(next.node);
    } else if (row && e.key === 'ArrowRight' && row.branch && !row.open) {
      e.preventDefault();
      toggle(row.node);
    } else if (row && e.key === 'ArrowLeft' && row.open) {
      e.preventDefault();
      toggle(row.node);
    }
  };

  return (
    <div ref={listRef} className="hx-kit-tree" role="tree" aria-label={label} tabIndex={0} onKeyDown={onKey}>
      <div
        className="hx-kit-tree__space"
        data-rows={rows.length}
        ref={(el) => el?.style.setProperty('--rows', String(rows.length))}
      >
        {rows.slice(from, to).map((r, k) => (
          <div
            key={r.node}
            role="treeitem"
            aria-level={r.depth + 1}
            aria-expanded={r.branch ? r.open : undefined}
            aria-selected={r.node === selected}
            className="hx-kit-tree__row"
            ref={(el) => {
              el?.style.setProperty('--row', String(from + k));
              el?.style.setProperty('--depth', String(r.depth));
            }}
            onClick={() => onSelect(r.node)}
          >
            {r.branch ? (
              <button
                type="button"
                className="hx-kit-tree__twist"
                data-open={r.open || undefined}
                aria-label={r.label}
                tabIndex={-1}
                onClick={(e) => {
                  e.stopPropagation();
                  toggle(r.node);
                }}
              >
                <Icon name="chevron-right" size="sm" />
              </button>
            ) : (
              <span className="hx-kit-tree__twist" />
            )}
            <span className="hx-kit-tree__label">{r.label}</span>
            {r.count !== undefined && <span className="hx-kit-tree__count">{r.count}</span>}
          </div>
        ))}
      </div>
    </div>
  );
};

export default KitTree;
