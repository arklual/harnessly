// The widgets under the chat's right column on a computer: small cards, each its name over what it shows,
// moved by hand, folded by a click on the name, removed from its «⋯». «+ Виджет» under them opens the gallery:
// harnessly's own widgets and the apps' (an agent makes one with `harnessly widget create`).
//
// What stays cheap: a card draws its body only while it is unfolded and on screen (an IntersectionObserver over the
// stack's own scroll), so a folded or scrolled-away card asks nothing and holds no frame; while away it keeps its last
// height, so nothing jumps. The stack is a layer of its own beside the feed (no layout of the feed depends on it), and
// its data come from their own stores, so typing and the feed's scroll never draw it.
import { Menu } from '@base-ui/react/menu';
import { memo, useEffect, useRef, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import useDrag, { EDGE } from '@/next/ui/use-drag';
import useFlip from '@/next/ui/use-flip';
import type { ISideWidget } from '@/next/features/widgets/model';
import '@/next/styles/widgets.css';

const DRAG = 'side-widget';
type TDrag = typeof DRAG;
const DRAGS: readonly TDrag[] = [DRAG];

export interface IWidgetLook {
  title: string;
  icon: string;
}

export interface IGalleryEntry {
  key: string;
  title: string;
  icon: string;
  /** One line of what it shows; the gallery's rows are the name over it. */
  hint?: string;
  onAdd: () => void;
}

export interface ISideWidgetsProps {
  widgets: readonly ISideWidget[];
  lookOf: (widget: ISideWidget) => IWidgetLook;
  /** The body of an unfolded card on screen; `on` is false never (the card does not draw it then). */
  renderBody: (widget: ISideWidget) => ReactNode;
  /** What the card's header carries besides the fold (a smart widget's «ask now»). */
  renderTool?: (widget: ISideWidget) => ReactNode;
  gallery: readonly IGalleryEntry[];
  onMove: (id: string, at: string, after: boolean) => void;
  onFold: (id: string, folded: boolean) => void;
  onRemove: (id: string) => void;
}

/** Whether an element is in the stack's view; true until the observer says, so a card does not blink empty. */
const useOnScreen = (el: HTMLElement | null, root: HTMLElement | null): boolean => {
  const [on, setOn] = useState(true);
  useEffect(() => {
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver(([entry]) => setOn(entry?.isIntersecting ?? true), {
      root,
      rootMargin: '48px 0px',
    });
    io.observe(el);
    return () => io.disconnect();
  }, [el, root]);
  return on;
};

/** The body's last height, kept on the card while the body is away (`--hx-wg-h`), so the stack does not jump. */
const useKeptHeight = (card: HTMLElement | null, body: HTMLElement | null) => {
  useEffect(() => {
    if (!card || !body || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => card.style.setProperty('--hx-wg-h', `${body.offsetHeight}px`));
    ro.observe(body);
    return () => ro.disconnect();
  }, [card, body]);
};

interface ICardProps {
  widget: ISideWidget;
  look: IWidgetLook;
  root: HTMLElement | null;
  renderBody: (widget: ISideWidget) => ReactNode;
  tool: ReactNode;
  onFold: (id: string, folded: boolean) => void;
  onRemove: (id: string) => void;
}

const Card = memo(({ widget, look, root, renderBody, tool, onFold, onRemove }: ICardProps) => {
  const [card, setCard] = useState<HTMLElement | null>(null);
  const [body, setBody] = useState<HTMLDivElement | null>(null);
  const onScreen = useOnScreen(card, root);
  const folded = widget.folded === true;
  const drawn = !folded && onScreen;
  useKeptHeight(card, drawn ? body : null);
  const menu = useMenuActions();
  return (
    <section
      ref={setCard}
      className="hx-wg"
      data-flip={widget.id}
      data-drag={DRAG}
      data-drag-id={widget.id}
      data-drop={DRAG}
      data-drop-id={widget.id}
      data-folded={folded || undefined}
      aria-label={look.title}
    >
      <header className="hx-wg__head">
        <button
          type="button"
          className="hx-wg__name"
          aria-expanded={!folded}
          onClick={() => onFold(widget.id, !folded)}
        >
          <Icon name={look.icon} size="sm" className="hx-wg__icon" />
          <span className="hx-wg__title">{look.title}</span>
          <Icon name="chevron-down" size="sm" className="hx-wg__chev" />
        </button>
        {!folded && tool}
        <MenuRoot actionsRef={menu}>
          <Menu.Trigger
            render={
              <button type="button" className="hx-wg__more" aria-label={t('next.widgets.menu')}>
                <Icon name="ellipsis" size="sm" />
              </button>
            }
          />
          <MenuPanel
            actions={menu}
            className="hx-cmenu"
            label={t('next.widgets.menu')}
            side="bottom"
            align="end"
          >
            <Menu.Item className="hx-cmenu__item" onClick={() => onFold(widget.id, !folded)}>
              <Icon name={folded ? 'chevron-down' : 'chevron-up'} size="sm" />
              <span className="hx-cmenu__txt">
                {folded ? t('next.widgets.unfold') : t('next.widgets.fold')}
              </span>
            </Menu.Item>
            <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={() => onRemove(widget.id)}>
              <Icon name="trash-2" size="sm" />
              <span className="hx-cmenu__txt">{t('next.widgets.remove')}</span>
            </Menu.Item>
          </MenuPanel>
        </MenuRoot>
      </header>
      <div className="hx-wg__clip" inert={folded || undefined}>
        <div className="hx-wg__keep" data-away={(!folded && !onScreen) || undefined}>
          {drawn && (
            <div ref={setBody} className="hx-wg__body">
              {renderBody(widget)}
            </div>
          )}
        </div>
      </div>
    </section>
  );
});
Card.displayName = 'SideWidgetCard';

const Gallery = ({ entries }: { entries: readonly IGalleryEntry[] }) => {
  const menu = useMenuActions();
  return (
    <MenuRoot actionsRef={menu}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-wg-add">
            <Icon name="plus" size="sm" />
            {t('next.widgets.add')}
          </button>
        }
      />
      <MenuPanel
        actions={menu}
        className="hx-cmenu hx-wg-gallery"
        label={t('next.widgets.gallery')}
        side="bottom"
        align="end"
      >
        {entries.length === 0 && <div className="hx-wg-gallery__empty">{t('next.widgets.galleryEmpty')}</div>}
        {entries.map((e) => (
          <Menu.Item key={e.key} className="hx-cmenu__item hx-wg-gallery__item" onClick={e.onAdd}>
            <Icon name={e.icon} size="sm" />
            <span className="hx-wg-gallery__txt">
              <span className="hx-wg-gallery__name">{e.title}</span>
              {e.hint && <span className="hx-wg-gallery__hint">{e.hint}</span>}
            </span>
          </Menu.Item>
        ))}
      </MenuPanel>
    </MenuRoot>
  );
};

const SideWidgets = ({
  widgets,
  lookOf,
  renderBody,
  renderTool,
  gallery,
  onMove,
  onFold,
  onRemove,
}: ISideWidgetsProps) => {
  const listRef = useRef<HTMLDivElement>(null);
  const [root, setRoot] = useState<HTMLDivElement | null>(null);
  useEffect(() => setRoot(listRef.current), []);
  useDrag<TDrag, TDrag, { id: string; at: string; after: boolean }>({
    listRef,
    dragKinds: DRAGS,
    dropKinds: DRAGS,
    lined: () => true,
    tracked: false,
    resolve: (source, target) =>
      target.id && target.id !== source.id && target.edge
        ? { id: source.id, at: target.id, after: target.edge === EDGE.after }
        : null,
    onDrop: (plan) => onMove(plan.id, plan.at, plan.after),
  });
  useFlip(listRef);
  return (
    <div className="hx-wgs">
      <div ref={listRef} className="hx-wgs__list">
        {widgets.map((w) => (
          <Card
            key={w.id}
            widget={w}
            look={lookOf(w)}
            root={root}
            renderBody={renderBody}
            tool={renderTool?.(w) ?? null}
            onFold={onFold}
            onRemove={onRemove}
          />
        ))}
      </div>
      <div className="hx-wgs__foot">
        <Gallery entries={gallery} />
      </div>
    </div>
  );
};

export default SideWidgets;
