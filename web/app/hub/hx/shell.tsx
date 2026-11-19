// The new hub's frame, the board «Хаб» one to one: the sidebar (brand, the organization · tenant switcher, the menu
// of the role, the person and the theme) beside the settings-like page where every screen draws itself.
import {
  createContext,
  Fragment,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';

import { useTheme } from '@kit';

import { navigate, useLocation } from '../lib/router';
import { useSession } from '../lib/session';
import { useSignOut } from '../screens/home';
import { useOrg } from '../org/context';
import { ROLE_LABEL } from '../org/types';
import { fleetWorkspace, liveOn } from './flag';
import { LiveCtx } from './live';
import { useHostUI } from './ext/host';
import { NAV_HINT, navOf } from './nav';
import { Btn, Icon, initial, LayerCtx, usePop } from './ui';
import { withBase } from '../lib/base';
import { canOpen } from './screens/machines/calc';
import { useHxTheme } from './theme';

// The organization's colour on the switcher's mark: one of the design's, stable for the organization.
const ORG_COLORS = ['#5b5bd6', '#2f9e6b', '#d97a1f', '#c2417a', '#3a7bd5', '#8968f4'];
const orgColor = (slug: string): string => {
  let h = 0;
  for (const ch of slug) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return ORG_COLORS[h % ORG_COLORS.length];
};

const ROLE_WORD: Record<keyof typeof ROLE_LABEL, string> = {
  admin: 'админ',
  lead: 'лид',
  member: 'участник',
};

/** The sidebar's switcher, as the design has it: organizations only; the team is picked in the page's head. */
const OrgSwitcher = ({ onPick }: { onPick: () => void }) => {
  const { shell, base } = useOrg();
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const org = shell.org;
  const go = (to: string) => {
    pop.close();
    onPick();
    navigate(to);
  };
  return (
    <div className="hb-ctx" ref={root}>
      <button
        type="button"
        className="hb-ctx__btn"
        aria-expanded={pop.open}
        aria-haspopup="menu"
        onClick={pop.toggle}
      >
        <span className="hb-ctx__mark" style={{ background: orgColor(org.slug) }}>
          {initial(org.name)}
        </span>
        <span className="hb-ctx__t">
          <b>{org.name}</b>
          <small>{ROLE_WORD[shell.viewer.role]}</small>
        </span>
        <Icon i="chevrons-up-down" size="sm" />
      </button>
      {pop.mounted && (
        <div className="hb-pop hb-pop--wide" data-closing={pop.attr} role="menu">
          <div className="hb-pop__label">Организации</div>
          {(shell.orgs.length
            ? shell.orgs
            : [{ slug: org.slug, name: org.name, roleLabel: ROLE_LABEL[shell.viewer.role] }]
          ).map((o) => (
            <button
              key={o.slug}
              type="button"
              className="hb-pop__item"
              role="menuitem"
              onClick={() => go(`/o/${o.slug}`)}
            >
              <span className="hb-ctx__mark hd-mark-sm" style={{ background: orgColor(o.slug) }}>
                {initial(o.name)}
              </span>
              <span style={{ flex: 1 }}>
                <b style={{ fontWeight: 560 }}>{o.name}</b>
                <br />
                <span className="mut2 sm">{o.roleLabel}</span>
              </span>
              {o.slug === org.slug && <Icon i="check" />}
            </button>
          ))}
          <div className="hb-pop__sep" />
          <button type="button" className="hb-pop__item" role="menuitem" onClick={() => go(`${base}/map`)}>
            <Icon i="circle-question-mark" />
            Что такое организация и команда
          </button>
        </div>
      )}
    </div>
  );
};

/** «Вся организация» in the page's head: what the screens show — the whole organization or one team (tenant). */
export const ScopeBtn = () => {
  const { shell, scope, section } = useOrg();
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const [q, setQ] = useState('');
  const admin = shell.viewer.role === 'admin';
  const isOrg = scope.kind === 'org';
  const name = isOrg ? 'Вся организация' : scope.tenant.name;
  const prefix = `/o/${shell.org.slug}`;
  // ⌘K / Ctrl+K opens it on its search.
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
  if (!admin && shell.tenants.length < 2) return null;
  const go = (slug: string) => {
    pop.close();
    navigate(`${prefix}/${slug}/${section}`);
  };
  const needle = q.trim().toLowerCase();
  const list = shell.tenants.filter((t) => !needle || t.name.toLowerCase().includes(needle));
  return (
    <div className="hb-tools hd-scope-wrap" ref={root} style={{ position: 'relative' }}>
      <button
        type="button"
        className="hx-btn hd-scope"
        data-variant="outline"
        data-size="sm"
        aria-expanded={pop.open}
        aria-haspopup="menu"
        onClick={pop.toggle}
        title="Что показывать: вся организация или одна команда. Меняет данные на всех экранах сразу."
      >
        <Icon i="users" size="sm" />
        <span>{name}</span>
        <Icon i="chevrons-up-down" size="sm" />
      </button>
      {pop.mounted && (
        <div
          className="hb-pop"
          data-closing={pop.attr}
          role="menu"
          style={{ left: 'auto', right: 0, width: 260, transformOrigin: 'top right' }}
        >
          {shell.tenants.length > 7 && (
            <label className="hb-pop__search">
              <Icon i="search" size="sm" />
              <input
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="Найти команду"
                aria-label="Найти команду"
              />
              <kbd>⌘K</kbd>
            </label>
          )}
          <div className="hb-pop__label">Показывать</div>
          {admin && !needle && (
            <button type="button" className="hb-pop__item" role="menuitem" onClick={() => go('all')}>
              <Icon i="users" />
              <span style={{ flex: 1 }}>Вся организация</span>
              {isOrg && <Icon i="check" />}
            </button>
          )}
          {list.map((t) => (
            <button
              key={t.id}
              type="button"
              className="hb-pop__item"
              role="menuitem"
              style={{ paddingLeft: admin ? 24 : undefined }}
              onClick={() => go(t.slug)}
            >
              <span
                className="hb-ctx__mark hd-mark-sm"
                style={{ width: 16, height: 16, borderRadius: 4, background: orgColor(t.slug) }}
              />
              <span style={{ flex: 1 }}>{t.name}</span>
              {!isOrg && scope.tenant.slug === t.slug && <Icon i="check" />}
            </button>
          ))}
          {needle && list.length === 0 && <div className="hb-pop__empty">Ничего не нашлось</div>}
        </div>
      )}
    </div>
  );
};

/**
 * The parts of a screen the sidebar opens straight away, under its item: the groups of «Аналитика» and the
 * main numbers of «Эффективность». They come with the lively hub (`ui.hx.live`).
 */
interface NavSub {
  id: string;
  label: string;
  /** The query the part is opened with; the rest of the address (the period) stays. */
  query?: Record<string, string | null>;
  /** The block of the screen to scroll to. */
  anchor?: string;
}

const SUBS: Partial<Record<string, readonly NavSub[]>> = {
  machines: [
    { id: 'chats', label: 'Для чатов', anchor: 'machines' },
    { id: 'builders', label: 'Билдеры', anchor: 'builders' },
  ],
  analytics: [
    { id: 'home', label: 'Главная', query: { s: null, k: null } },
    { id: 'money', label: 'Деньги', query: { s: 'money', k: null } },
    { id: 'res', label: 'Результат', query: { s: 'res', k: null } },
    { id: 'agent', label: 'Работа агента', query: { s: 'agent', k: null } },
    { id: 'env', label: 'Контекст и кэш', query: { s: 'env', k: null } },
  ],
  efficiency: [
    { id: 'price', label: 'Цена MR', query: { t: null, k: null } },
    { id: 'mrs', label: 'Смёржено MR', query: { t: 'mrs', k: null } },
    { id: 'waste', label: 'Потери', query: { t: 'waste', k: null } },
    { id: 'limit', label: 'Лимит недели', query: { t: 'limit', k: null } },
  ],
};

const scrollToPart = (anchor: string) => {
  // The screen may still be loading its chunk: try for a moment, then give up quietly.
  let tries = 0;
  const step = () => {
    const el = document.querySelector(`[data-hx-part="${anchor}"]`);
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      return;
    }
    if (++tries < 30) window.setTimeout(step, 100);
  };
  step();
};

const Nav = ({ onPick }: { onPick: () => void }) => {
  const { shell, base, section, scope } = useOrg();
  const { search } = useLocation();
  const team = shell.teams.find((t) => t.id === shell.viewer.teamId);
  const ui = useHostUI();
  const hasViews = (ui?.views.length ?? 0) > 0;
  // An item the person folded or unfolded by hand; otherwise the item of the open screen is unfolded.
  const [folded, setFolded] = useState<Record<string, boolean>>({});
  const groups = useMemo(
    () => navOf(shell.viewer.role, team?.name ?? null, shell.features ?? [], scope.kind, hasViews),
    [shell.viewer.role, team?.name, shell.features, scope.kind, hasViews],
  );
  const q = new URLSearchParams(search);
  const openSub = (sec: string, sub: NavSub) => {
    onPick();
    const next = sec === section ? new URLSearchParams(search) : new URLSearchParams();
    for (const [k, v] of Object.entries(sub.query ?? {})) {
      if (v === null) next.delete(k);
      else next.set(k, v);
    }
    const qs = next.toString();
    navigate(`${base}/${sec}${qs ? `?${qs}` : ''}`);
    if (sub.anchor) scrollToPart(sub.anchor);
  };
  const subOn = (sec: string, sub: NavSub): boolean =>
    sec === section && !!sub.query && Object.entries(sub.query).every(([k, v]) => q.get(k) === v);
  const anyOn = (sec: string): boolean => (SUBS[sec] ?? []).some((sub) => subOn(sec, sub));
  return (
    <nav className="hx-nav hb-hubnav" aria-label="Разделы хаба">
      {groups.map((g) => (
        <div key={g.label || 'main'}>
          {g.label && <div className="hx-group__label hb-static hb-hubnav__h">{g.label}</div>}
          <div className="hx-nav__group">
            {g.items.map((it, k) => {
              // The page of one team is a part of «Люди и команды» for the admin.
              const on = it.section === section || (it.section === 'teams' && section === 'team');
              const subs =
                it.section && !(it.section === 'machines' && shell.viewer.role === 'member')
                  ? SUBS[it.section]
                  : undefined;
              const open = subs ? (folded[it.id] ?? on) : false;
              const btn = (
                <button
                  type="button"
                  className="hx-nav__item"
                  style={{ ['--k' as string]: k }}
                  aria-current={on ? 'page' : 'false'}
                  title={it.section ? NAV_HINT[it.section] : undefined}
                  onClick={() => {
                    if (!it.section) return;
                    if (subs) setFolded((f) => ({ ...f, [it.id]: true }));
                    onPick();
                    navigate(`${base}/${it.section}`);
                  }}
                >
                  <span className="hx-nav__ic">
                    <Icon i={it.icon} />
                  </span>
                  <span className="hx-nav__label">{it.label}</span>
                </button>
              );
              if (!subs || !it.section) return <Fragment key={it.id}>{btn}</Fragment>;
              const sec = it.section;
              return (
                <Fragment key={it.id}>
                  <div className={open ? 'hd-navrow is-open' : 'hd-navrow'}>
                    {btn}
                    <button
                      type="button"
                      className="hd-navchev"
                      aria-expanded={open}
                      aria-label={open ? `Свернуть «${it.label}»` : `Развернуть «${it.label}»`}
                      onClick={() => setFolded((f) => ({ ...f, [it.id]: !open }))}
                    >
                      <Icon i="chevron-down" size="sm" />
                    </button>
                  </div>
                  {open && (
                    <div className="an-sub">
                      {subs.map((sub, j) => (
                        <button
                          key={sub.id}
                          type="button"
                          className="an-sub__i"
                          aria-current={
                            subOn(sec, sub) || (sec === section && !anyOn(sec) && j === 0) ? 'page' : 'false'
                          }
                          onClick={() => openSub(sec, sub)}
                        >
                          {sub.label}
                        </button>
                      ))}
                    </div>
                  )}
                </Fragment>
              );
            })}
          </div>
        </div>
      ))}
    </nav>
  );
};

/** «Как устроен хаб»: one page on what the hub is made of (the board's map). */
const HelpNav = ({ onPick }: { onPick: () => void }) => {
  const { base, section } = useOrg();
  return (
    <div className="hx-nav hd-helpnav">
      <button
        type="button"
        className="hx-nav__item"
        aria-current={section === 'map' ? 'page' : 'false'}
        title="Одна страница: из чего состоит хаб и куда делись старые «пространства» и «тенанты»."
        onClick={() => {
          onPick();
          navigate(`${base}/map`);
        }}
      >
        <span className="hx-nav__ic">
          <Icon i="book-open" />
        </span>
        <span className="hx-nav__label">Как устроен хаб</span>
      </button>
    </div>
  );
};

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
          title="Ваша личная кнопка: все ваши машины и чаты в одном окне."
        >
          <span className="hx-nav__ic">
            <Icon i={busy ? 'loader-circle' : 'square-terminal'} />
          </span>
          <span className="hx-nav__label">{busy ? 'Открываю…' : 'Рабочее пространство'}</span>
          <Icon i="arrow-up-right" size="sm" className="hd-ws-go" />
        </button>
      </div>
    </div>
  );
};

const User = ({ onPick }: { onPick: () => void }) => {
  const { shell, base } = useOrg();
  const { resolved, setMode } = useTheme();
  const { viewer } = shell;
  const signOut = useSignOut();
  const root = useRef<HTMLDivElement>(null);
  const pop = usePop(root);
  const flip = () => setMode(resolved === 'dark' ? 'light' : 'dark');
  return (
    <div className="hx-user" ref={root} style={{ position: 'relative' }}>
      <button
        type="button"
        className="hx-user__open"
        aria-expanded={pop.open}
        aria-haspopup="menu"
        onClick={pop.toggle}
        title="Ваш профиль, способы входа, тема, выход."
      >
        <span className="hx-avatar hd-av hd-av--me">{initial(viewer.name)}</span>
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
        title="Светлая или тёмная. Запоминается для вас."
        onClick={flip}
      >
        <Icon i={resolved === 'dark' ? 'sun' : 'moon'} size="sm" />
      </Btn>
      {pop.mounted && (
        <div
          className="hb-pop hd-pop"
          data-closing={pop.attr}
          role="menu"
          style={{
            left: 0,
            right: 'auto',
            top: 'auto',
            bottom: 'calc(100% + 6px)',
            width: 300,
            transformOrigin: 'bottom left',
          }}
        >
          <div className="hb-pop__label">
            {viewer.name} · {ROLE_LABEL[viewer.role]}
          </div>
          <button
            type="button"
            className="hb-pop__item"
            role="menuitem"
            onClick={() => {
              pop.close();
              onPick();
              navigate(`${base}/profile`);
            }}
          >
            <Icon i="user" />
            Профиль и вход
          </button>
          <button type="button" className="hb-pop__item" role="menuitem" onClick={flip}>
            <Icon i="sun" />
            Тема: {resolved === 'dark' ? 'тёмная' : 'светлая'}
          </button>
          <div className="hb-pop__sep" />
          <button type="button" className="hb-pop__item" role="menuitem" onClick={signOut}>
            <Icon i="arrow-right" />
            Выйти
          </button>
        </div>
      )}
    </div>
  );
};

/** The logo of the board: the dark square with «h» and the lime dot. */
const Logo = () => (
  <svg className="hx-logo" viewBox="0 0 1024 1024" aria-hidden="true">
    <rect width="1024" height="1024" rx="224" fill="#0B0D10" />
    <path
      transform="translate(235.1 712.2)"
      fill="#F2F2EE"
      d="M29.12 -400.4H135.72V0H29.12ZM244.4 -305.76Q287.56 -305.76 317.46 -288.6Q347.36 -271.44 363.48 -240.24Q379.6 -209.04 379.6 -166.4V0H273V-150.8Q273 -184.08 256.1 -201.76Q239.2 -219.44 207.48 -219.44Q185.64 -219.44 169.26 -210.08Q152.88 -200.72 144.3 -183.56Q135.72 -166.4 135.72 -143L102.44 -160.16Q108.68 -207.48 128.96 -239.98Q149.24 -272.48 179.14 -289.12Q209.04 -305.76 244.4 -305.76Z"
    />
    <circle cx="715.6" cy="668" r="44.2" fill="#D4FF3A" />
  </svg>
);

/** The phone menu's state, for the burger in each screen's head. */
const SideCtx = createContext<(open: boolean) => void>(() => undefined);

export const HxShell = ({ children }: { children: ReactNode }) => {
  const theme = useHxTheme();
  const [layer, setLayer] = useState<HTMLDivElement | null>(null);
  const { section, shell } = useOrg();
  const live = liveOn(shell.features);
  // The phone menu is open for the screen it was opened on: going to another screen closes it.
  const [sideOn, setSideOn] = useState<string | null>(null);
  const side = sideOn === section;
  const setSide = useCallback((open: boolean) => setSideOn(open ? section : null), [section]);
  const close = () => setSide(false);
  return (
    <LiveCtx.Provider value={live}>
      <SideCtx.Provider value={setSide}>
        <div className="hx-root hb hd-host" data-hx-theme={theme} data-live={live ? '1' : undefined}>
          <div className="hx-app hd-app" data-side-m={side ? 'open' : ''}>
            <aside className="hx-sidebar hd-side">
              <div className="hx-brand">
                <span className="hx-brand__mark hx-brand__mark--logo">
                  <Logo />
                </span>
                <span className="hx-brand__name">harnessly</span>
                <span className="hx-spacer" />
                <span className="hb-hubtag">хаб</span>
              </div>
              <OrgSwitcher onPick={close} />
              <Nav onPick={close} />
              <span className="hx-spacer" />
              <HelpNav onPick={close} />
              <Workspace onPick={close} />
              <User onPick={close} />
            </aside>
            <div className="hd-sidescrim" onClick={close} />
            <main className="hx-main hd-main" data-view="settings">
              <section className="hx-set" data-page="true">
                <div className="hx-set__page hd-scroll">
                  <div className="hx-set__page-in hd-page" data-route={section}>
                    <LayerCtx.Provider value={layer}>{children}</LayerCtx.Provider>
                  </div>
                </div>
              </section>
            </main>
          </div>
          <div className="hxh-layer" ref={setLayer} />
        </div>
      </SideCtx.Provider>
    </LiveCtx.Provider>
  );
};

/**
 * A screen's head as the board draws it (`head()`): the phone burger, the title and its lead, and on the right the
 * scope (whole organization or one team) followed by the screen's own controls.
 */
export const Head = ({
  title,
  lead,
  acts,
  scope = true,
}: {
  title: ReactNode;
  lead?: ReactNode;
  acts?: ReactNode;
  scope?: boolean;
}) => (
  <div className="hb-head hd-head">
    <Titles title={title} lead={lead} scope={scope} />
    {acts && <div className="hb-tools">{acts}</div>}
  </div>
);

/**
 * A screen's title row inside its `.hb-head`, as the board's `head()`: the phone burger, the title with its lead and,
 * on the right, the scope (whole organization or one team). The screen's own controls follow it in the head.
 */
export const Titles = ({
  title,
  lead,
  end,
  scope = true,
}: {
  title: ReactNode;
  lead?: ReactNode;
  end?: ReactNode;
  scope?: boolean;
}) => {
  const setSide = useContext(SideCtx);
  return (
    <>
      <Btn className="hd-burger" variant="ghost" shape="icon" aria-label="Меню" onClick={() => setSide(true)}>
        <Icon i="menu" />
      </Btn>
      <div className="hx-set__titles">
        <h2 className="hx-set__title">{title}</h2>
        {lead != null && <p className="hx-set__lead">{lead}</p>}
        {end}
      </div>
      {scope && <ScopeBtn />}
    </>
  );
};
