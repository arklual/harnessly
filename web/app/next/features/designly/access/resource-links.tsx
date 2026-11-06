// «Ссылки без входа» of the one dialog (stage «г»): a link of its own for each recipient, view or comment,
// never edit. The list says for whom, what it allows, until when and whether it was opened; a link's menu copies it
// (or gives it a new address where the old one was not kept), changes it, switches it off or on again. A link is run
// by its maker or by a manager: someone else's link has no menu (the gateway refuses it anyway). The screen of a link
// is one field and rows of which one is open at a time: «Доступ», «Срок», «Скачивание», «Копирование текста» (a
// document), «Ещё» by kind. A made link is copied at once.
import { useCallback, useEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react';
import { copied } from '@/next/features/designly/access/access-link-rows';
import { copyText } from '@/next/features/designly/access/copy-link';
import {
  blankForm,
  changeOf,
  downloadOf,
  formOf,
  inputOf,
  isOff,
  linkSub,
  LINK_KIND,
  moreSummary,
  readOnly,
  termDate,
  TERMS,
  type ILinkContext,
  type ILinkForm,
  type TTerm,
} from '@/next/features/designly/access/link-form';
import { untilLabel } from '@/next/features/designly/access/labels';
import {
  changeLink,
  createLink,
  linkAddress,
  reissueLink,
  type IAccessLink,
  type IResourceRef,
  type IResourceView,
} from '@/next/features/designly/access/resource-api';
import type { IAction } from '@/next/features/designly/access/use-action';
import { DECK_OPENS } from '@/next/features/designly/deck/types';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { useLeavingList } from '@/next/ui/presence';
import Switch from '@/next/ui/switch';
import Tooltip from '@/next/ui/tooltip';
import { t } from '@/next/i18n';

const OUT_MS = 200;
const FRESH_MS = 2200;
const FOCUS_MS = 280;
const NOW_MS = 30_000;
const NO_LINKS: IAccessLink[] = [];

/** The clock, read again every so often: «открывали сегодня» turns into «вчера» while the dialog stays open. */
const useNow = () => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), NOW_MS);
    return () => clearInterval(timer);
  }, []);
  return now;
};

export interface ILinksScreen {
  /** The link being changed; null for a new one. */
  editing: IAccessLink | null;
  form: ILinkForm;
  setForm: (patch: Partial<ILinkForm>) => void;
  /** Starts the screen for a new link or for a link to change. */
  start: (link: IAccessLink | null) => void;
  /** Makes or saves the link; true when it is done (the screen then leaves). */
  submit: () => Promise<boolean>;
  /** The link just made or saved, marked in the list for a moment. */
  fresh: { id: string; copied: boolean } | null;
}

/** The state of the screen of a link and what it sends; the gateway's answer comes back into the view. */
export const useLinksScreen = ({
  target,
  view,
  ctx,
  action,
  onNext,
}: {
  target: IResourceRef;
  view: IResourceView;
  ctx: ILinkContext;
  action: IAction;
  onNext: (next: IResourceView) => void;
}): ILinksScreen => {
  const [editing, setEditing] = useState<IAccessLink | null>(null);
  const [form, setFormState] = useState<ILinkForm>(() => blankForm(ctx));
  const [fresh, setFresh] = useState<ILinksScreen['fresh']>(null);
  const timer = useRef(0);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const mark = (id: string, wasCopied: boolean) => {
    window.clearTimeout(timer.current);
    setFresh({ id, copied: wasCopied });
    timer.current = window.setTimeout(() => setFresh(null), FRESH_MS);
  };

  const put = (link: IAccessLink) => {
    const links = view.links ?? NO_LINKS;
    const known = links.some((l) => l.id === link.id);
    onNext({ ...view, links: known ? links.map((l) => (l.id === link.id ? link : l)) : [link, ...links] });
  };

  const submit = () =>
    action.run('link-save', async () => {
      if (form.forWhom.trim() === '') throw new Error(t('next.access.links.whoMissing'));
      if (editing) {
        const link = await changeLink(target, editing.id, changeOf(form, ctx));
        put(link);
        mark(link.id, false);
        return;
      }
      // The copy starts in the press (a browser keeps the permission of the click for it) and takes the address once made.
      const made = createLink(target, inputOf(form, ctx));
      const copy = copyText(made.then((answer) => answer.url));
      let link: IAccessLink;
      try {
        link = (await made).link;
      } catch (failure) {
        copy.catch(() => undefined);
        throw failure;
      }
      put(link);
      mark(link.id, true);
      await copied(copy);
    });

  // Kept the same between renders: the list of links takes them, and it should not be drawn again as the form changes.
  const setForm = useCallback(
    (patch: Partial<ILinkForm>) => setFormState((now) => ({ ...now, ...patch })),
    [],
  );
  const start = useCallback(
    (link: IAccessLink | null) => {
      setEditing(link);
      setFormState(link ? formOf(link, ctx) : blankForm(ctx));
    },
    [ctx],
  );

  return { editing, form, setForm, start, submit, fresh };
};

const LinkMenu = ({
  link,
  target,
  action,
  onEdit,
  onLink,
  now,
}: {
  link: IAccessLink;
  target: IResourceRef;
  action: IAction;
  onEdit: () => void;
  onLink: (link: IAccessLink) => void;
  now: number;
}) => {
  const [open, setOpen] = useState(false);
  const off = isOff(link, now);
  const label = t('next.access.guest.actions');
  const idle = action.busy === null;
  const close = (then: () => void) => () => {
    setOpen(false);
    then();
  };
  const copy = () =>
    void action.run(`copy:${link.id}`, async () => {
      const address = linkAddress(target, link.id).then((url) => {
        if (url === null) throw new Error(t('next.access.links.notKept'));
        return url;
      });
      await copied(copyText(address));
    });
  const reissue = () =>
    void action.run(`reissue:${link.id}`, async () => {
      const made = reissueLink(target, link.id);
      await copied(copyText(made));
      onLink({ ...link, copyable: true });
    });
  const turn = (disabled: boolean) =>
    void action.run(`turn:${link.id}`, async () => onLink(await changeLink(target, link.id, { disabled })));
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={label}
      side="bottom"
      align="end"
      className="hx-acc-pop"
      trigger={
        <button type="button" className="hx-acc-dots" aria-label={label} disabled={!idle}>
          <Icon name="ellipsis" size="sm" />
        </button>
      }
    >
      <div role="menu" aria-label={label} className="hx-acc-menu">
        {!off && link.copyable && (
          <button type="button" role="menuitem" className="hx-acc-mi" onClick={close(copy)}>
            <Icon name="copy" size="sm" className="hx-acc-mi__icon" />
            <span className="hx-acc-mi__text">{t('next.access.links.copy')}</span>
          </button>
        )}
        {!off && !link.copyable && (
          <button type="button" role="menuitem" className="hx-acc-mi" onClick={close(reissue)}>
            <Icon name="rotate-cw" size="sm" className="hx-acc-mi__icon" />
            <span className="hx-acc-mi__text">
              <span>{t('next.access.links.reissue')}</span>
              <small className="hx-acc-mi__desc">{t('next.access.links.reissueNote')}</small>
            </span>
          </button>
        )}
        {!off && (
          <button type="button" role="menuitem" className="hx-acc-mi" onClick={close(onEdit)}>
            <Icon name="pencil" size="sm" className="hx-acc-mi__icon" />
            <span className="hx-acc-mi__text">{t('next.access.links.edit')}</span>
          </button>
        )}
        {off ? (
          <button type="button" role="menuitem" className="hx-acc-mi" onClick={close(() => turn(false))}>
            <Icon name="rotate-ccw" size="sm" className="hx-acc-mi__icon" />
            <span className="hx-acc-mi__text">{t('next.access.links.on')}</span>
          </button>
        ) : (
          <button
            type="button"
            role="menuitem"
            className="hx-acc-mi"
            data-danger="true"
            onClick={close(() => turn(true))}
          >
            <Icon name="ban" size="sm" className="hx-acc-mi__icon" />
            <span className="hx-acc-mi__text">
              <span>{t('next.access.links.offDo')}</span>
              <small className="hx-acc-mi__desc">{t('next.access.links.offNote')}</small>
            </span>
          </button>
        )}
      </div>
    </Popover>
  );
};

/** The list of links with «+ Новая ссылка» over it. */
export const ResourceLinks = ({
  target,
  view,
  action,
  onNext,
  ctx,
  me,
  screen,
  onOpen,
}: {
  target: IResourceRef;
  view: IResourceView;
  action: IAction;
  onNext: (next: IResourceView) => void;
  ctx: ILinkContext;
  me: string | null;
  /** What the list needs of the link screen: how to start it, and the link just made. */
  screen: Pick<ILinksScreen, 'start' | 'fresh'>;
  /** Opens the screen of a link (after `screen.start`). */
  onOpen: () => void;
}) => {
  const now = useNow();
  const rows = useLeavingList(view.links ?? NO_LINKS, (l) => l.id, OUT_MS);
  const mine = (link: IAccessLink) =>
    view.manager || (me !== null && link.createdBy.toLowerCase() === me.toLowerCase());
  const put = (link: IAccessLink) =>
    onNext({ ...view, links: (view.links ?? NO_LINKS).map((l) => (l.id === link.id ? link : l)) });
  const tip = t('next.access.links.tip');
  return (
    <section className="hx-acc-sec" data-section="links">
      <div className="hx-acc-sec__head">
        <h3 className="hx-acc-sec__title">{t('next.access.guestLinks')}</h3>
        <Tooltip label={tip}>
          <span className="hx-acc-help" tabIndex={0} aria-label={tip}>
            <Icon name="info" size="sm" />
          </span>
        </Tooltip>
        <button
          type="button"
          className="hx-acc-sec__act"
          onClick={() => {
            screen.start(null);
            onOpen();
          }}
        >
          <Icon name="plus" size="sm" />
          <span>{t('next.access.linkNew')}</span>
        </button>
      </div>
      {rows.length > 0 && (
        <ul className="hx-acc-rows">
          {rows.map(({ item: link, leaving }) => {
            const off = isOff(link, now);
            const fresh = screen.fresh?.id === link.id;
            return (
              <li
                key={link.id}
                className="hx-acc-row hx-acc-link"
                data-leaving={leaving || undefined}
                data-off={off || undefined}
                data-fresh={fresh || undefined}
              >
                <span className="hx-acc-av hx-acc-link__ic" data-live={!off || undefined} aria-hidden="true">
                  <Icon name={off ? 'eye-off' : 'link'} size="sm" />
                </span>
                <span className="hx-acc-row__who">
                  <span className="hx-acc-row__nm hx-acc-link__nm">{link.forWhom}</span>
                  <span className="hx-acc-row__sub">{linkSub(link, ctx.kind, now)}</span>
                </span>
                {fresh && screen.fresh?.copied && (
                  <em className="hx-acc-link__ok">
                    <Icon name="check" size="sm" />
                    {t('next.access.links.copiedMark')}
                  </em>
                )}
                {mine(link) && (
                  <LinkMenu
                    link={link}
                    target={target}
                    action={action}
                    now={now}
                    onLink={put}
                    onEdit={() => {
                      screen.start(link);
                      onOpen();
                    }}
                  />
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
};

const ACC = { role: 'role', term: 'term', more: 'more' } as const;
type TAcc = (typeof ACC)[keyof typeof ACC];

const Row = ({
  name,
  value,
  open,
  onToggle,
  children,
}: {
  name: string;
  value: string;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
}) => (
  <>
    <button type="button" className="hx-acc-set" aria-expanded={open} onClick={onToggle}>
      <span className="hx-acc-set__name">{name}</span>
      <em className="hx-acc-set__value">{value}</em>
      <Icon name="chevron-down" size="sm" className="hx-acc-set__chev" />
    </button>
    <div className="hx-acc-drop" data-open={open || undefined} inert={!open}>
      <div>
        <div className="hx-acc-drop__in">{children}</div>
      </div>
    </div>
  </>
);

const SwitchRow = ({
  name,
  note,
  on,
  onChange,
  off = false,
}: {
  name: string;
  note?: string;
  on: boolean;
  onChange: (on: boolean) => void;
  off?: boolean;
}) => (
  <div className="hx-acc-set hx-acc-set--sw" data-off={off || undefined}>
    <span className="hx-acc-set__name">
      <span>{name}</span>
      {note && <small>{note}</small>}
    </span>
    <Switch label={name} checked={on && !off} disabled={off} onChange={onChange} />
  </div>
);

interface ITile<V extends string | number> {
  value: V;
  name: string;
  sub?: string;
  icon?: string;
  shape?: 'w' | 't';
}

const Tiles = <V extends string | number>({
  label,
  tiles,
  value,
  onPick,
}: {
  label: string;
  tiles: readonly ITile<V>[];
  value: V;
  onPick: (value: V) => void;
}) => (
  <div className="hx-acc-tiles" role="radiogroup" aria-label={label} data-n={tiles.length}>
    {tiles.map((tile) => (
      <button
        key={tile.value}
        type="button"
        role="radio"
        className="hx-acc-tile"
        aria-checked={tile.value === value}
        onClick={() => onPick(tile.value)}
      >
        {tile.icon && <Icon name={tile.icon} />}
        {tile.shape && <span className="hx-acc-tile__shape" data-r={tile.shape} aria-hidden="true" />}
        <b>{tile.name}</b>
        {tile.sub && <small>{tile.sub}</small>}
      </button>
    ))}
  </div>
);

const Block = ({ label, children }: { label?: string; children: ReactNode }) => (
  <div className="hx-acc-blk">
    {label && <div className="hx-acc-blk__h">{label}</div>}
    {children}
  </div>
);

/** «Срок»: a slider of four stops (a day, a week, a month, for ever) and the date it gives. */
const TermSlider = ({
  term,
  touched,
  until,
  onPick,
  now,
}: {
  term: TTerm;
  touched: boolean;
  until: string | null;
  onPick: (term: TTerm) => void;
  now: number;
}) => {
  const [drag, setDrag] = useState(false);
  const at = touched ? TERMS.indexOf(term) : -1;
  const pct = (i: number) => (i * 100) / (TERMS.length - 1);
  const stopAt = (event: PointerEvent<HTMLDivElement>): TTerm => {
    const box = event.currentTarget.getBoundingClientRect();
    const x = Math.min(1, Math.max(0, (event.clientX - box.left) / box.width));
    return TERMS[Math.round(x * (TERMS.length - 1))];
  };
  const name = (stop: TTerm) => (stop === null ? '∞' : t(`next.access.links.term.${stop}`));
  const valueText = at < 0 ? (until ?? '') : termDate(term, now);
  return (
    <>
      <div
        className="hx-acc-range"
        data-drag={drag || undefined}
        data-set={at >= 0 || undefined}
        role="slider"
        tabIndex={0}
        aria-label={t('next.access.links.termLabel')}
        aria-valuemin={0}
        aria-valuemax={TERMS.length - 1}
        aria-valuenow={Math.max(0, at)}
        aria-valuetext={valueText}
        // The slider's place is a share of its own width: a custom property, not a style rule written here.
        ref={(node) => node?.style.setProperty('--hx-at', `${pct(Math.max(0, at))}%`)}
        onKeyDown={(event) => {
          const step = KEY_STEP[event.key];
          if (step === undefined) return;
          event.preventDefault();
          onPick(TERMS[Math.min(TERMS.length - 1, Math.max(0, (at < 0 ? 1 : at) + step))]);
        }}
        onPointerDown={(event) => {
          event.currentTarget.setPointerCapture(event.pointerId);
          setDrag(true);
          onPick(stopAt(event));
        }}
        onPointerMove={(event) => {
          if (!drag) return;
          const next = stopAt(event);
          if (next !== term || !touched) onPick(next);
        }}
        onPointerUp={() => setDrag(false)}
        onPointerCancel={() => setDrag(false)}
      >
        <span className="hx-acc-range__track" />
        <span className="hx-acc-range__fill" />
        {TERMS.map((stop, i) => (
          <span
            key={String(stop)}
            className="hx-acc-range__stop"
            data-on={(at >= 0 && (i <= at || term === null)) || undefined}
            data-i={i}
          />
        ))}
        <span className="hx-acc-range__thumb" />
      </div>
      <div className="hx-acc-range__lbls">
        {TERMS.map((stop, i) => (
          <button
            key={String(stop)}
            type="button"
            data-i={i}
            aria-pressed={i === at}
            title={stop === null ? t('next.access.ttl.forever') : undefined}
            onClick={() => onPick(stop)}
          >
            {name(stop)}
          </button>
        ))}
      </div>
      <p className="hx-acc-range__date" key={valueText}>
        <Icon name="clock" size="sm" />
        {valueText}
      </p>
    </>
  );
};

/** «Срок» on its closed row: the stop chosen, or the end of a link being changed while its term is not touched. */
const termWords = (form: ILinkForm, editing: IAccessLink | null): string => {
  if (editing && !form.termTouched) {
    if (editing.expiresAt === null) return t('next.access.ttl.forever');
    return untilLabel(editing.expiresAt) ?? '';
  }
  if (form.term === null) return t('next.access.ttl.forever');
  return t(`next.access.links.termName.${form.term}`);
};

const KEY_STEP: Record<string, number> = { ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1 };

/** «Ещё» of a link, by kind. */
const More = ({
  form,
  setForm,
  ctx,
}: {
  form: ILinkForm;
  setForm: ILinksScreen['setForm'];
  ctx: ILinkContext;
}) => {
  const version = (
    <Block label={t('next.access.links.versionLabel')}>
      <Tiles
        label={t('next.access.links.versionLabel')}
        value={form.version}
        onPick={(v) => setForm({ version: v })}
        tiles={[
          {
            value: 'pinned',
            name: t('next.access.links.version.pinned'),
            icon: 'pin',
            sub: t('next.access.links.versionPinnedSub'),
          },
          {
            value: 'latest',
            name: t('next.access.links.version.latest'),
            icon: 'rotate-cw',
            sub: t('next.access.links.versionLatestSub'),
          },
        ]}
      />
    </Block>
  );
  if (ctx.kind === LINK_KIND.design) {
    const pages = ctx.pages ?? [];
    const picked = form.pages ?? pages.map((p) => p.path);
    const flip = (path: string) => {
      const next = picked.includes(path) ? picked.filter((p) => p !== path) : [...picked, path];
      // At least one page stays; all of them is «all» (null), so a page added later is given too.
      if (next.length === 0) return;
      setForm({
        pages: next.length === pages.length ? null : pages.map((p) => p.path).filter((p) => next.includes(p)),
      });
    };
    return (
      <>
        <Block label={t('next.access.links.deviceLabel')}>
          <Tiles
            label={t('next.access.links.deviceLabel')}
            value={form.device}
            onPick={(device) => setForm({ device })}
            tiles={[
              { value: 'desktop', name: t('next.access.links.device.desktop'), icon: 'monitor' },
              { value: 'phone', name: t('next.access.links.device.phone'), icon: 'smartphone' },
            ]}
          />
        </Block>
        {pages.length > 1 && (
          <Block label={t('next.access.links.pagesLabel')}>
            <div className="hx-acc-chips">
              {pages.map((page) => (
                <button
                  key={page.path}
                  type="button"
                  className="hx-acc-pchip"
                  aria-pressed={picked.includes(page.path)}
                  onClick={() => flip(page.path)}
                >
                  <Icon name="check" size="sm" />
                  {page.title}
                </button>
              ))}
            </div>
          </Block>
        )}
        {version}
      </>
    );
  }
  if (ctx.kind === LINK_KIND.deck) {
    const slides = Math.max(1, ctx.slides ?? 1);
    return (
      <>
        <Block label={t('next.access.links.opensLabel')}>
          <Tiles
            label={t('next.access.links.opensLabel')}
            value={form.opens}
            onPick={(opens) => setForm({ opens })}
            tiles={[
              { value: DECK_OPENS.slides, name: t('next.access.links.opens.slides'), icon: 'layout-grid' },
              { value: DECK_OPENS.full, name: t('next.access.links.opens.full'), icon: 'maximize-2' },
              { value: DECK_OPENS.video, name: t('next.access.links.opens.video'), icon: 'play' },
            ]}
          />
        </Block>
        {slides > 1 && (
          <Block label={t('next.access.links.fromLabel')}>
            <div className="hx-acc-strip">
              {Array.from({ length: slides }, (_, i) => i + 1).map((n) => (
                <button
                  key={n}
                  type="button"
                  className="hx-acc-slide"
                  aria-pressed={form.from === n}
                  aria-label={t('next.access.links.slideN', { n })}
                  onClick={() => setForm({ from: n })}
                >
                  {n}
                </button>
              ))}
            </div>
          </Block>
        )}
        <Block>
          <SwitchRow
            name={t('next.access.links.notes')}
            on={form.notes}
            onChange={(notes) => setForm({ notes })}
          />
          <SwitchRow
            name={t('next.access.links.stats')}
            on={form.stats}
            onChange={(stats) => setForm({ stats })}
          />
        </Block>
        {version}
      </>
    );
  }
  if (ctx.kind === LINK_KIND.video) {
    const both = (ctx.formats?.length ?? 0) > 1;
    return (
      <>
        {both && (
          <Block label={t('next.access.links.formatLabel')}>
            <Tiles
              label={t('next.access.links.formatLabel')}
              value={form.format}
              onPick={(format) => setForm({ format })}
              tiles={[
                { value: '16:9', name: '16:9', sub: t('next.access.links.formatWide'), shape: 'w' },
                { value: '9:16', name: '9:16', sub: t('next.access.links.formatTall'), shape: 't' },
              ]}
            />
          </Block>
        )}
        <Block>
          <SwitchRow
            name={t('next.access.links.autoplay')}
            on={form.autoplay}
            onChange={(autoplay) => setForm({ autoplay })}
          />
          <SwitchRow
            name={t('next.access.links.tlComments')}
            note={form.rights === 'comment' ? undefined : t('next.access.links.tlCommentsNote')}
            on={form.tlComments}
            off={form.rights !== 'comment'}
            onChange={(tlComments) => setForm({ tlComments })}
          />
        </Block>
        {version}
      </>
    );
  }
  return version;
};

/** The screen of a link: «Для кого» and the rows. Its button is the dialog's foot (`form` attribute). */
export const LinkScreen = ({
  id,
  screen,
  ctx,
  onSubmit,
}: {
  id: string;
  screen: ILinksScreen;
  ctx: ILinkContext;
  onSubmit: () => void;
}) => {
  const { form, setForm, editing } = screen;
  const [acc, setAcc] = useState<TAcc | null>(null);
  const who = useRef<HTMLInputElement>(null);
  // A new link starts at «Для кого», once the screen has slid in.
  useEffect(() => {
    if (editing) return;
    const timer = window.setTimeout(() => who.current?.focus({ preventScroll: true }), FOCUS_MS);
    return () => window.clearTimeout(timer);
  }, [editing]);
  const now = useNow();
  const toggle = (next: TAcc) => () => setAcc((was) => (was === next ? null : next));
  const file = downloadOf(ctx.kind);
  const what = t(`next.access.links.what.${ctx.kind}`);
  const termValue = termWords(form, editing);
  return (
    <form
      id={id}
      className="hx-acc-linkform"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <input
        ref={who}
        className="hx-acc-forwhom"
        type="text"
        value={form.forWhom}
        maxLength={200}
        placeholder={t('next.access.links.whoPlaceholder')}
        aria-label={t('next.access.links.whoLabel')}
        onChange={(event) => setForm({ forWhom: event.target.value })}
      />
      <Row
        name={t('next.access.links.rightsLabel')}
        value={t(`next.access.links.rightsValue.${form.rights}`)}
        open={acc === ACC.role}
        onToggle={toggle(ACC.role)}
      >
        <Tiles
          label={t('next.access.links.rightsLabel')}
          value={form.rights}
          onPick={(rights) => setForm({ rights })}
          tiles={[
            {
              value: 'view',
              name: t('next.access.links.rightsValue.view'),
              icon: 'eye',
              sub: t('next.access.links.viewSub', { what }),
            },
            {
              value: 'comment',
              name: t('next.access.links.rightsValue.comment'),
              icon: 'message-square',
              sub: t('next.access.links.commentSub'),
            },
          ]}
        />
      </Row>
      <Row
        name={t('next.access.links.termLabel')}
        value={termValue}
        open={acc === ACC.term}
        onToggle={toggle(ACC.term)}
      >
        <TermSlider
          term={form.term}
          touched={form.termTouched || !editing}
          until={editing ? termValue : null}
          now={now}
          onPick={(term) => setForm({ term, termTouched: true })}
        />
      </Row>
      {file !== null && (
        <SwitchRow
          name={t('next.access.links.download')}
          note={file}
          on={form.download}
          onChange={(download) => setForm({ download })}
        />
      )}
      {ctx.kind === LINK_KIND.doc && (
        <>
          <SwitchRow
            name={t('next.access.links.copyText')}
            note={form.copy ? t('next.access.links.copyOn') : t('next.access.links.copyOff')}
            on={form.copy}
            onChange={(copy) => setForm({ copy })}
          />
          {readOnly(form, ctx.kind) && (
            <p className="hx-acc-ro-note">
              <Icon name="eye" size="sm" />
              {t('next.access.links.readOnly')}
            </p>
          )}
        </>
      )}
      <Row
        name={t('next.access.links.more')}
        value={moreSummary(form, ctx)}
        open={acc === ACC.more}
        onToggle={toggle(ACC.more)}
      >
        <div className="hx-acc-drop__scroll">
          <More form={form} setForm={setForm} ctx={ctx} />
        </div>
      </Row>
    </form>
  );
};
