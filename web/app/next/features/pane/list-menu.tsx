// The menu of the empty part of the list in «Файлы» (right click or a long press): a new folder, a new file, files to
// upload. It opens at the pointer, closes with Escape, a click away or a pick, and plays its exit (spec §6).
import { useEffect, useRef, type CSSProperties } from 'react';
import Icon, { type TIconName } from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

export interface IListMenuItem {
  icon: TIconName;
  label: string;
  onPick: () => void;
}

export interface IListMenuProps {
  /** Where it opens (client coordinates); null keeps it closed. */
  at: { x: number; y: number } | null;
  items: readonly IListMenuItem[];
  onClose: () => void;
}

const EXIT_MS = 180;
/** The menu is 236 px wide and about 40 px per item; it stays inside the window. */
const WIDTH = 236;
const ITEM_H = 34;

const ListMenu = ({ at, items, onClose }: IListMenuProps) => {
  const { value, state } = usePresence(at, EXIT_MS);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (at === null) return undefined;
    const away = (event: Event) => {
      if (!root.current?.contains(event.target as Node)) onClose();
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('pointerdown', away, true);
    document.addEventListener('keydown', key);
    return () => {
      document.removeEventListener('pointerdown', away, true);
      document.removeEventListener('keydown', key);
    };
  }, [at, onClose]);
  useEffect(() => {
    if (at !== null) root.current?.querySelector<HTMLElement>('button')?.focus();
  }, [at]);
  if (value === null) return null;
  const x = Math.max(8, Math.min(value.x, window.innerWidth - WIDTH - 8));
  const y = Math.max(8, Math.min(value.y, window.innerHeight - items.length * ITEM_H - 24));
  return (
    <div
      ref={root}
      className="hx-cmenu hx-lmenu"
      role="menu"
      aria-label={t('next.files.listMenu')}
      data-ending-style={state === PRESENCE.closing || undefined}
      style={{ '--mx': `${x}px`, '--my': `${y}px` } as CSSProperties}
    >
      {items.map((item) => (
        <button
          key={item.label}
          type="button"
          role="menuitem"
          className="hx-cmenu__item"
          onClick={() => {
            onClose();
            item.onPick();
          }}
        >
          <Icon name={item.icon} size="sm" />
          <span className="hx-cmenu__txt">{item.label}</span>
        </button>
      ))}
    </div>
  );
};

export default ListMenu;
