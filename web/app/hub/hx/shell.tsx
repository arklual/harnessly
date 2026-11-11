// The new hub's frame, the board «Хаб» one to one: the sidebar (brand, the organization · tenant switcher, the menu
// of the role, the person and the theme) beside the settings-like page where every screen draws itself.
import { type ReactNode, useEffect, useMemo, useRef, useState } from 'react';

import { useTheme } from '@kit';

import { navigate } from '../lib/router';
import { useSession } from '../lib/session';
import { useSignOut } from '../screens/home';
import { useOrg } from '../org/context';
import { ROLE_LABEL, type TenantRef } from '../org/types';
import { fleetWorkspace, liveOn } from './flag';
import { LiveCtx } from './live';
import { useHostUI } from './ext/host';
import { navOf } from './nav';
import { Btn, Icon, initial, LayerCtx, usePop } from './ui';
import { withBase } from '../lib/base';
import { canOpen } from './screens/machines/calc';
import { useHxTheme } from './theme';

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
    <div className="hb-ctx" ref={root}>
      <button
        type="button"
        className="hb-ctx__btn"
        aria-expanded={pop.open}
        aria-haspopup="menu"
        onClick={pop.toggle}
      >
        <span className="hb-ctx__mark" data-org={isOrg ? '1' : '0'}>
          {initial(title)}
        </span>
        <span className="hb-ctx__t">
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
      className="hb-pop__item"
      role="menuitem"
      onClick={() => go(`${prefix}/${t.slug}/overview`)}
    >
      <span className="hb-dot">{initial(t.name)}</span>
      <span>{t.name}</span>
      {t.slug === here ? <Icon i="check" size="sm" /> : pin ? <Icon i="pin" size="sm" /> : null}
    </button>
  );
  return (
    <div className="hb-pop hb-pop--wide" data-closing={closing} role="menu">
      <label className="hb-pop__search">
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
          className="hb-pop__item hb-pop__org"
          role="menuitem"
          onClick={() => go(`${prefix}/all/overview`)}
        >
          <span className="hb-dot hb-dot--org">
            <Icon i="layers" size="sm" />
          </span>
          <span>Вся организация</span>
          {isOrg && <Icon i="check" size="sm" />}
        </button>
      )}
      {recent.length > 0 && <div className="hb-pop__label">Недавние</div>}
      {recent.map((t) => row(t, false))}
      {pinned.length > 0 && <div className="hb-pop__label">Закреплённые</div>}
      {pinned.map((t) => row(t, true))}
      {needle && recent.length === 0 && pinned.length === 0 && (
        <div className="hb-pop__empty">Ничего не нашлось</div>
      )}
      {!needle && admin && (
        <>
          <div className="hb-pop__sep" />
          <button
            type="button"
            className="hb-pop__item"
            data-muted="1"
            role="menuitem"
            onClick={() => go(`${prefix}/all/overview`)}
          >
            <Icon i="layout-grid" size="sm" />
            <span>Все тенанты</span>
          </button>
          <button
            type="button"
            className="hb-pop__item"
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
          <div className="hb-pop__sep" />
          <div className="hb-pop__label">Организации</div>
          {shell.orgs.map((o) => (
            <button
              key={o.slug}
              type="button"
              className="hb-pop__item"
              role="menuitem"
              onClick={() => go(`/o/${o.slug}`)}
            >
              <span className="hb-dot">{initial(o.name)}</span>
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
  const { shell, base, section, scope } = useOrg();
  const team = shell.teams.find((t) => t.id === shell.viewer.teamId);
  const ui = useHostUI();
  const hasViews = (ui?.views.length ?? 0) > 0;
  const groups = useMemo(
    () => navOf(shell.viewer.role, team?.name ?? null, shell.features ?? [], scope.kind, hasViews),
    [shell.viewer.role, team?.name, shell.features, scope.kind, hasViews],
  );
  return (
    <nav className="hx-nav hb-hubnav" aria-label="Разделы хаба">
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
    {label && <div className="hx-group__label hb-static hb-hubnav__h">{label}</div>}
    {children}
  </>
);

/**
 * «Рабочее пространство»: back to where the person works with agents. Through the gateway that is its /agents/, and so
 * it is on a hub of its own that serves the fleet (`runtime.fleet`): one workspace over every machine the person may
 * open. Otherwise the person's machine when it has exactly one that is on, or its machines to pick from.
 */
const Workspace = ({ onPick }: { onPick: () => void }) => {
  const { config } = useSession();
  const { api, base, scope, shell } = useOrg();
  const [busy, setBusy] = useState(false);
  const go = async () => {
    if (config.proxied || fleetWorkspace(config.workspace, shell.features)) {
      onPick();
      window.location.assign(withBase('/agents/'));
      return;
    }
    setBusy(true);
    try {
      const slug = scope.kind === 'org' ? 'all' : scope.tenant.slug;
      const list = await api.machinesList(shell.org.slug, { scope: slug });
      const on = list.groups.flatMap((g) => g.machines).filter((m) => m.mine && canOpen(m));
      if (on.length === 1) {
        const { url } = await api.openMachine(on[0].id);
        window.location.assign(withBase(url));
        return;
      }
    } catch {
      // The list of machines shows what is wrong better than this button could.
    } finally {
      setBusy(false);
    }
    onPick();
    navigate(`${base}/machines`);
  };
  return (
    <div className="hx-nav hb-hubnav hx-wsnav">
      <div className="hx-nav__group">
        <button
          type="button"
          className="hx-nav__item"
          onClick={() => void go()}
          disabled={busy}
          aria-busy={busy}
          title="Открыть свою машину с агентами"
        >
          <span className="hx-nav__ic">
            <Icon i={busy ? 'loader-circle' : 'square-terminal'} />
          </span>
          <span className="hx-nav__label">{busy ? 'Открываю…' : 'Рабочее пространство'}</span>
        </button>
      </div>
    </div>
  );
};

// The sidebar footer is narrow: the role goes there in one word, the full title stays in the tooltip.
const SHORT_ROLE: Record<keyof typeof ROLE_LABEL, string> = {
  admin: 'Админ',
  lead: 'Руководитель',
  member: 'Участник',
};

const User = () => {
  const { shell } = useOrg();
  const { resolved, setMode } = useTheme();
  const { viewer } = shell;
  const signOut = useSignOut();
  return (
    <div className="hx-user">
      <button
        type="button"
        className="hx-user__open"
        onClick={() => navigate('/settings')}
        aria-label={`${viewer.name}, ${ROLE_LABEL[viewer.role]}`}
        title={`${viewer.name}\n${ROLE_LABEL[viewer.role]}`}
      >
        <span className="hx-avatar">{initial(viewer.name)}</span>
        <span className="hx-user__text">
          <span className="hx-user__name">{viewer.name}</span>
          <span className="hx-user__meta">{SHORT_ROLE[viewer.role]}</span>
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
      <Btn
        className="hx-user__theme"
        variant="ghost"
        size="sm"
        shape="icon"
        aria-label="Выйти"
        onClick={signOut}
      >
        <Icon i="log-out" />
      </Btn>
    </div>
  );
};

export const HxShell = ({ children }: { children: ReactNode }) => {
  const theme = useHxTheme();
  const [layer, setLayer] = useState<HTMLDivElement | null>(null);
  const { section, shell } = useOrg();
  const live = liveOn(shell.features);
  // The phone menu is open for the screen it was opened on: going to another screen closes it.
  const [sideOn, setSideOn] = useState<string | null>(null);
  const side = sideOn === section;
  const setSide = (open: boolean) => setSideOn(open ? section : null);
  return (
    <LiveCtx.Provider value={live}>
      <div
        className="hx-root hx-app hb"
        data-hx-theme={theme}
        data-live={live ? '1' : undefined}
        data-hxh-side={side ? 'open' : undefined}
      >
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
            <span className="hb-hubtag">хаб</span>
          </div>
          <Switcher />
          <Nav onPick={() => setSide(false)} />
          <div className="hx-spacer" />
          <Workspace onPick={() => setSide(false)} />
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
    </LiveCtx.Provider>
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
