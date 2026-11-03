// The new hub's frame, the board «Хаб» one to one: the sidebar (brand, the organization · tenant switcher, the menu
// of the role, the person and the theme) beside the settings-like page where every screen draws itself.
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';

import { useTheme } from '@kit';

import { navigate } from '../lib/router';
import { useOrg } from '../org/context';
import { ROLE_LABEL, type TenantRef } from '../org/types';
import { navOf } from './nav';
import { Btn, Icon, initial, LayerCtx, usePop } from './ui';

const Switcher = () => {
  const { shell, scope } = useOrg();
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const isOrg = scope.kind === 'org';
  const title = isOrg ? shell.org.name : scope.tenant.name;
  const sub = isOrg ? 'вся организация' : shell.org.name;

  // ⌘K / Ctrl+K opens the switcher on its search, as the board's hint says.
  const { setOpen } = pop;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen(true);
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [setOpen]);

  return (
    <div className="ad-ctx" ref={root}>
      <button
        type="button"
        className="ad-ctx__btn"
        aria-expanded={pop.open}
        aria-haspopup="menu"
        onClick={pop.toggle}
      >
        <span className="ad-ctx__mark" data-org={isOrg ? '1' : '0'}>
          {initial(title)}
        </span>
        <span className="ad-ctx__t">
          <b>{title}</b>
          <small>{sub}</small>
        </span>
        <Icon i="chevrons-up-down" size="sm" />
      </button>
      {pop.mounted && <SwitcherPop closing={pop.attr} onPick={pop.close} />}
    </div>
  );
};

/** The switcher's list; it lives only while open, so the search starts empty every time. */
const SwitcherPop = ({ closing, onPick }: { closing: '1' | undefined; onPick: () => void }) => {
  const { shell, scope } = useOrg();
  const [q, setQ] = useState('');
  const search = useRef<HTMLInputElement>(null);
  useEffect(() => search.current?.focus({ preventScroll: true }), []);
  const admin = shell.viewer.role === 'admin';
  const isOrg = scope.kind === 'org';
  const here = isOrg ? null : scope.tenant.slug;
  const prefix = `/o/${shell.org.slug}`;
  const go = (to: string) => {
    onPick();
    navigate(to);
  };
  const needle = q.trim().toLowerCase();
  const match = (t: TenantRef) => !needle || t.name.toLowerCase().includes(needle);
  const recent = shell.tenants.filter((t) => (t.recent || (!t.pinned && !needle)) && match(t));
  const pinned = shell.tenants.filter((t) => t.pinned && match(t));
  const row = (t: TenantRef, pin: boolean) => (
    <button
      key={`${pin ? 'p' : 'r'}-${t.id}`}
      type="button"
      className="ad-pop__item"
      role="menuitem"
      onClick={() => go(`${prefix}/${t.slug}/overview`)}
    >
      <span className="ad-dot">{initial(t.name)}</span>
      <span>{t.name}</span>
      {t.slug === here ? <Icon i="check" size="sm" /> : pin ? <Icon i="pin" size="sm" /> : null}
    </button>
  );
  return (
    <div className="ad-pop ad-pop--wide" data-closing={closing} role="menu">
      <label className="ad-pop__search">
        <Icon i="search" size="sm" />
        <input
          ref={search}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Найти тенант"
          aria-label="Найти тенант"
        />
        <kbd>⌘K</kbd>
      </label>
      {admin && !needle && (
        <button
          type="button"
          className="ad-pop__item ad-pop__org"
          role="menuitem"
          onClick={() => go(`${prefix}/all/overview`)}
        >
          <span className="ad-dot ad-dot--org">
            <Icon i="layers" size="sm" />
          </span>
          <span>Вся организация</span>
          {isOrg && <Icon i="check" size="sm" />}
        </button>
      )}
      {recent.length > 0 && <div className="ad-pop__label">Недавние</div>}
      {recent.map((t) => row(t, false))}
      {pinned.length > 0 && <div className="ad-pop__label">Закреплённые</div>}
      {pinned.map((t) => row(t, true))}
      {needle && recent.length === 0 && pinned.length === 0 && (
        <div className="ad-pop__empty">Ничего не нашлось</div>
      )}
      {!needle && admin && (
        <>
          <div className="ad-pop__sep" />
          <button
            type="button"
            className="ad-pop__item"
            data-muted="1"
            role="menuitem"
            onClick={() => go(`${prefix}/all/overview`)}
          >
            <Icon i="layout-grid" size="sm" />
            <span>Все тенанты</span>
          </button>
          <button
            type="button"
            className="ad-pop__item"
            data-muted="1"
            role="menuitem"
            onClick={() => go(`/new?org=${encodeURIComponent(shell.org.slug)}`)}
          >
            <Icon i="plus" size="sm" />
            <span>Новый тенант</span>
          </button>
        </>
      )}
      {!needle && shell.orgs.length > 0 && (
        <>
          <div className="ad-pop__sep" />
          <div className="ad-pop__label">Организации</div>
          {shell.orgs.map((o) => (
            <button
              key={o.slug}
              type="button"
              className="ad-pop__item"
              role="menuitem"
              onClick={() => go(`/o/${o.slug}`)}
            >
              <span className="ad-dot">{initial(o.name)}</span>
              <span>{o.name}</span>
              <small>{o.roleLabel}</small>
            </button>
          ))}
        </>
      )}
    </div>
  );
};

const Nav = ({ onPick }: { onPick: () => void }) => {
  const { shell, base, section } = useOrg();
  const team = shell.teams.find((t) => t.id === shell.viewer.teamId);
  const groups = useMemo(
    () => navOf(shell.viewer.role, team?.name ?? null, shell.features ?? []),
    [shell.viewer.role, team?.name, shell.features],
  );
  return (
    <nav className="hx-nav ad-hubnav" aria-label="Разделы хаба">
      {groups.map((g) => (
        <GroupItems key={g.label || 'main'} label={g.label}>
          {g.items.map((it) => {
            // The page of one team is a part of «Команды» for the admin, whose menu has no item of its own for it.
            const on = it.section === section || (it.section === 'teams' && section === 'team');
            const off = !it.section;
            return (
              <div className="hx-nav__group" key={it.id}>
                <button
                  type="button"
                  className="hx-nav__item"
                  aria-current={on ? 'page' : 'false'}
                  aria-disabled={off ? 'true' : 'false'}
                  title={off ? 'Скоро' : undefined}
                  onClick={() => {
                    if (!it.section) return;
                    onPick();
                    navigate(`${base}/${it.section}`);
                  }}
                >
                  <span className="hx-nav__ic">
                    <Icon i={it.icon} />
                  </span>
                  <span className="hx-nav__label">{it.label}</span>
                </button>
              </div>
            );
          })}
        </GroupItems>
      ))}
    </nav>
  );
};

const GroupItems = ({ label, children }: { label: string; children: ReactNode }) => (
  <>
    {label && <div className="hx-group__label ad-static ad-hubnav__h">{label}</div>}
    {children}
  </>
);

const User = () => {
  const { shell } = useOrg();
  const { resolved, setMode } = useTheme();
  const { viewer } = shell;
  return (
    <div className="hx-user">
      <button
        type="button"
        className="hx-user__open"
        onClick={() => navigate('/account')}
        aria-label={`${viewer.name}, ${ROLE_LABEL[viewer.role]}`}
      >
        <span className="hx-avatar">{initial(viewer.name)}</span>
        <span className="hx-user__text">
          <span className="hx-user__name">{viewer.name}</span>
          <span className="hx-user__meta">{ROLE_LABEL[viewer.role]}</span>
        </span>
      </button>
      <Btn
        className="hx-user__theme"
        variant="ghost"
        size="sm"
        shape="icon"
        aria-label="Сменить тему"
        onClick={() => setMode(resolved === 'dark' ? 'light' : 'dark')}
      >
        <Icon i={resolved === 'dark' ? 'sun' : 'moon'} />
      </Btn>
    </div>
  );
};

/** The page's theme for the design's tokens: data-hx-theme on <html> (the body's colour) and on the root. */
const useHxTheme = (): 'light' | 'dark' => {
  const { resolved } = useTheme();
  useEffect(() => {
    const html = document.documentElement;
    html.dataset.hxTheme = resolved;
    return () => {
      delete html.dataset.hxTheme;
    };
  }, [resolved]);
  return resolved;
};

export const HxShell = ({ children }: { children: ReactNode }) => {
  const theme = useHxTheme();
  const [layer, setLayer] = useState<HTMLDivElement | null>(null);
  const { section } = useOrg();
  // The phone menu is open for the screen it was opened on: going to another screen closes it.
  const [sideOn, setSideOn] = useState<string | null>(null);
  const side = sideOn === section;
  const setSide = (open: boolean) => setSideOn(open ? section : null);
  return (
    <div className="hx-root hx-app ad" data-hx-theme={theme} data-hxh-side={side ? 'open' : undefined}>
      <Btn
        className="hxh-menu"
        variant="ghost"
        size="sm"
        shape="icon"
        aria-label="Меню"
        onClick={() => setSide(true)}
      >
        <Icon i="menu" />
      </Btn>
      <div className="hxh-side-scrim" onClick={() => setSide(false)} />
      <aside className="hx-sidebar">
        <div className="hx-brand">
          <span className="hx-brand__mark">
            <span className="hx-orb" />
          </span>
          <span className="hx-brand__name">harnessly</span>
          <span className="hx-spacer" />
          <span className="ad-hubtag">хаб</span>
        </div>
        <Switcher />
        <Nav onPick={() => setSide(false)} />
        <div className="hx-spacer" />
        <User />
      </aside>
      <main className="hx-main" data-view="settings">
        <section className="hx-set" data-page="true">
          <div className="hx-set__page">
            <div className="hx-set__page-in">
              <LayerCtx.Provider value={layer}>{children}</LayerCtx.Provider>
            </div>
          </div>
        </section>
      </main>
      <div className="hxh-layer" ref={setLayer} />
    </div>
  );
};

/** A screen's title row: `.hx-set__titles` with the lead line and, on the right, the screen's own controls. */
export const Titles = ({ title, lead, end }: { title: ReactNode; lead?: ReactNode; end?: ReactNode }) => (
  <div className="hx-set__titles">
    <h2 className="hx-set__title">{title}</h2>
    {lead != null && <p className="hx-set__lead">{lead}</p>}
    {end}
  </div>
);

/** A screen of the menu that is not built yet, the way the board shows it. */
export const Stub = ({ title, lead }: { title: string; lead: string }) => (
  <>
    <Titles title={title} lead={lead} />
    <div className="ad-empty">Этот экран нарисуем следующим</div>
  </>
);
