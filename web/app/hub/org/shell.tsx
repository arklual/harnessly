// The frame of the organization hub: the sidebar (brand, the organization · tenant switcher, the menu of the role,
// the person) beside a white page. On a phone the sidebar is a drawer behind a menu button — one implementation.
import { type ReactNode, useMemo, useRef, useState } from 'react';
import {
  Activity,
  ChartColumn,
  Check,
  ChevronsUpDown,
  KeyRound,
  Layers,
  Layers2,
  Menu,
  Moon,
  Pin,
  Plug,
  Search,
  Server,
  Sun,
  Users,
} from 'lucide-react';

import { IconButton, Popup, useTheme } from '@kit';

import { Link, navigate } from '../lib/router';
import { useOrg } from './context';
import { navFor, type NavItem } from './nav';
import { ROLE_LABEL, type TenantRef } from './types';
import { useClosing } from './ui/closing';

const ICONS: Record<NavItem['icon'], ReactNode> = {
  overview: <ChartColumn size={16} />,
  analytics: <Activity size={16} />,
  machines: <Server size={16} />,
  teams: <Users size={16} />,
  env: <Layers size={16} />,
  accounts: <KeyRound size={16} />,
  connections: <Plug size={16} />,
};

const initial = (name: string) => name.slice(0, 1).toUpperCase();

const Switcher = () => {
  const { shell, scope, base } = useOrg();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const anchor = useRef<HTMLButtonElement>(null);
  const { mounted, closing } = useClosing(open, 140);
  const admin = shell.viewer.role === 'admin';
  const title = scope.kind === 'org' ? shell.org.name : scope.tenant.name;
  const sub = scope.kind === 'org' ? 'вся организация' : shell.org.name;
  const close = () => {
    setOpen(false);
    setQ('');
  };
  const go = (to: string) => {
    close();
    navigate(to);
  };
  const match = (t: TenantRef) => t.name.toLowerCase().includes(q.trim().toLowerCase());
  const recent = shell.tenants.filter((t) => t.recent && match(t));
  const pinned = shell.tenants.filter((t) => t.pinned && match(t));
  const prefix = `/o/${shell.org.slug}`;
  const here = base.split('/')[3] ?? 'all';
  const row = (t: TenantRef) => (
    <button
      key={t.id}
      type="button"
      className="ho-pop__item"
      onClick={() => go(`${prefix}/${t.slug}/overview`)}
    >
      <span className="ho-mark ho-mark--soft">{initial(t.name)}</span>
      <span className="ho-pop__name">{t.name}</span>
      {t.slug === here && <Check size={14} aria-label="Текущий" />}
      {t.pinned && t.slug !== here && <Pin size={13} className="ho-pop__pin" aria-hidden="true" />}
    </button>
  );
  return (
    <div className="ho-ctx">
      <button
        ref={anchor}
        type="button"
        className="ho-ctx__btn"
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => (open ? close() : setOpen(true))}
      >
        <span className="ho-mark">{initial(title)}</span>
        <span className="ho-ctx__t">
          <b>{title}</b>
          <small>{sub}</small>
        </span>
        <ChevronsUpDown size={14} aria-hidden="true" />
      </button>
      <Popup
        anchor={anchor}
        open={mounted}
        onClose={close}
        className="ho-pop"
        role="dialog"
        placement={{ side: 'bottom', align: 'start' }}
      >
        <div className="ho-pop__in" data-closing={closing || undefined}>
          {shell.tenants.length > 3 && (
            <label className="ho-pop__search">
              <Search size={14} aria-hidden="true" />
              <input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Найти тенант"
                aria-label="Найти тенант"
              />
            </label>
          )}
          {admin && !q && (
            <button type="button" className="ho-pop__item" onClick={() => go(`${prefix}/all/overview`)}>
              <span className="ho-mark ho-mark--dark">
                <Layers2 size={14} />
              </span>
              <span className="ho-pop__name">Вся организация</span>
              {here === 'all' && <Check size={14} aria-label="Текущий" />}
            </button>
          )}
          {recent.length > 0 && <p className="ho-pop__label">Недавние</p>}
          {recent.map(row)}
          {pinned.length > 0 && <p className="ho-pop__label">Закреплённые</p>}
          {pinned.map(row)}
          {recent.length === 0 && pinned.length === 0 && q && (
            <p className="ho-pop__empty">Ничего не нашлось</p>
          )}
          {shell.orgs.length > 1 && !q && (
            <>
              <p className="ho-pop__label">Организации</p>
              {shell.orgs.map((o) => (
                <button
                  key={o.slug}
                  type="button"
                  className="ho-pop__item"
                  onClick={() => go(`/o/${o.slug}`)}
                >
                  <span className="ho-mark ho-mark--soft">{initial(o.name)}</span>
                  <span className="ho-pop__name">{o.name}</span>
                  <span className="ho-pop__role">{o.roleLabel}</span>
                </button>
              ))}
            </>
          )}
        </div>
      </Popup>
    </div>
  );
};

const Menus = ({ onPick }: { onPick: () => void }) => {
  const { shell, scope, base, section } = useOrg();
  const { viewer } = shell;
  const team = shell.teams.find((t) => t.id === viewer.teamId);
  const groups = useMemo(
    () => navFor(viewer.role, scope.kind === 'org' ? 'Вся организация' : 'Тенант', team?.name ?? null),
    [viewer.role, scope.kind, team?.name],
  );
  return (
    <nav className="ho-nav" aria-label="Разделы">
      {groups.map((g, i) => (
        <div key={g.label ?? i} className="ho-nav__group">
          {g.label && <p className="ho-nav__label">{g.label}</p>}
          {g.items.map((it) =>
            it.section ? (
              <Link
                key={it.id}
                to={`${base}/${it.section}`}
                className="ho-nav__item"
                aria-current={it.section === section ? 'page' : undefined}
                onClick={onPick}
              >
                {ICONS[it.icon]}
                {it.label}
              </Link>
            ) : (
              <span key={it.id} className="ho-nav__item" aria-disabled="true">
                {ICONS[it.icon]}
                {it.label}
              </span>
            ),
          )}
        </div>
      ))}
    </nav>
  );
};

const Person = () => {
  const { shell } = useOrg();
  const { resolved, setMode } = useTheme();
  const { viewer } = shell;
  return (
    <div className="ho-me">
      <span className="ho-mark ho-mark--dark">{initial(viewer.name)}</span>
      <span className="ho-me__t">
        <b>{viewer.name}</b>
        <small>{ROLE_LABEL[viewer.role]}</small>
      </span>
      <IconButton
        label={resolved === 'dark' ? 'Светлая тема' : 'Тёмная тема'}
        onClick={() => setMode(resolved === 'dark' ? 'light' : 'dark')}
      >
        {resolved === 'dark' ? <Sun size={16} /> : <Moon size={16} />}
      </IconButton>
    </div>
  );
};

const SidebarBody = ({ onPick }: { onPick: () => void }) => (
  <>
    <div className="ho-brand">
      <span className="ho-orb" aria-hidden="true" />
      harnessly
      <span className="ho-brand__tag">хаб</span>
    </div>
    <Switcher />
    <Menus onPick={onPick} />
    <Person />
  </>
);

export const OrgShell = ({ children }: { children: ReactNode }) => {
  const [drawer, setDrawer] = useState(false);
  const { mounted, closing } = useClosing(drawer, 200);
  return (
    <div className="ho">
      <aside className="ho-side">
        <SidebarBody onPick={() => undefined} />
      </aside>
      <div className="ho-bar">
        <IconButton label="Меню" onClick={() => setDrawer(true)}>
          <Menu size={18} />
        </IconButton>
        <span className="ho-orb" aria-hidden="true" />
        <b>harnessly</b>
      </div>
      {mounted && (
        <div className="ho-drawer" data-closing={closing || undefined}>
          <div className="ho-drawer__scrim" onClick={() => setDrawer(false)} />
          <aside className="ho-drawer__panel" aria-label="Меню">
            <SidebarBody onPick={() => setDrawer(false)} />
          </aside>
        </div>
      )}
      <main className="ho-main">{children}</main>
    </div>
  );
};
