// An app as the main thing (board «Приложение — главное»): the app's view fills the page and the chat it works in stands
// beside it in one of three places — a column at the left, a window over the app, or only its field at the foot. The
// place is the viewer's choice and is remembered. This is the layout; what fills it (the chat, the view, the island's
// field) comes from the binding.
import { Menu } from '@base-ui/react/menu';
import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react';
import {
  clampFloat,
  FLOAT_EDGES,
  resizeFloat,
  type TFloatEdge,
  firstFloat,
  PLACEMENT,
  PLACEMENTS,
  type IFloat,
  type ISection,
  type TPlacement,
} from '@/next/features/app-main/placement';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

/** The exit of a place's chat, as long as the transitions of .hx-appmain__col, __float and __island. */
export const PLACE_EXIT_MS = 240;

const PLACEMENT_ICON = {
  left: 'panel-left',
  float: 'picture-in-picture-2',
  island: 'panel-bottom',
} as const;

/** The switch of where the chat stands. */
export const PlacementSwitch = ({
  value,
  onChange,
}: {
  value: TPlacement;
  onChange: (placement: TPlacement) => void;
}) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--seg-i', String(PLACEMENTS.indexOf(value)));
  }, [value]);
  return (
    <div ref={ref} className="hx-appmain__modes" role="radiogroup" aria-label={t('next.appmain.placement')}>
      <span className="hx-appmain__glider" />
      {PLACEMENTS.map((id) => (
        <button
          key={id}
          type="button"
          className="hx-appmain__mode hx-tipped"
          role="radio"
          aria-checked={id === value}
          aria-label={t(`next.appmain.placement.${id}`)}
          onClick={() => onChange(id)}
        >
          <Icon name={PLACEMENT_ICON[id]} size="sm" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {t(`next.appmain.placement.${id}`)}
          </span>
        </button>
      ))}
    </div>
  );
};

export interface IAppMenuItem {
  id: string;
  icon: string;
  label: string;
  onSelect: () => void;
  danger?: boolean;
}

/** «…» of the page: about the app, its settings, the debug section, turning the app off or on in this chat. */
const AppMenu = ({ items }: { items: readonly IAppMenuItem[] }) => {
  const actions = useMenuActions();
  if (items.length === 0) return null;
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-head-btn" aria-label={t('next.appmain.menu')}>
            <Icon name="ellipsis" />
          </button>
        }
      />
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={t('next.appmain.menu')}
        side="bottom"
        align="end"
      >
        {items.map((item) => (
          <Menu.Item
            key={item.id}
            className="hx-cmenu__item"
            data-danger={item.danger || undefined}
            onClick={item.onSelect}
          >
            <Icon name={item.icon} size="sm" />
            <span className="hx-cmenu__txt">{item.label}</span>
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};

interface IFloatWindowProps {
  title: string;
  float: IFloat;
  section: ISection;
  state: 'open' | 'closing';
  onFloat: (float: IFloat) => void;
  onLive: (float: IFloat | null) => void;
  children: ReactNode;
}

/** The window: moved by its head, grown by any edge or corner (the one at the foot on the right shows a grip); the place is kept when the pointer lets go. */
const FloatWindow = ({ title, float, section, state, onFloat, onLive, children }: IFloatWindowProps) => {
  const ref = useRef<HTMLElement>(null);
  // The place goes to the stylesheet as variables (no inline styles): .hx-appmain__float reads them.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.setProperty('--fx', `${float.x}px`);
    el.style.setProperty('--fy', `${float.y}px`);
    el.style.setProperty('--fw', `${float.w}px`);
    el.style.setProperty('--fh', `${float.h}px`);
  }, [float]);
  const grab = (event: ReactPointerEvent<HTMLElement>, mode: 'move' | TFloatEdge) => {
    if (event.button !== 0) return;
    event.preventDefault();
    const target = event.currentTarget;
    target.setPointerCapture(event.pointerId);
    const from = { x: event.clientX, y: event.clientY };
    let last = float;
    const move = (e: PointerEvent) => {
      const dx = e.clientX - from.x;
      const dy = e.clientY - from.y;
      last =
        mode === 'move'
          ? clampFloat({ ...float, x: float.x + dx, y: float.y + dy }, section)
          : resizeFloat(float, mode, dx, dy, section);
      onLive(last);
    };
    const end = () => {
      target.removeEventListener('pointermove', move);
      target.removeEventListener('pointerup', end);
      target.removeEventListener('pointercancel', end);
      onLive(null);
      onFloat(last);
    };
    target.addEventListener('pointermove', move);
    target.addEventListener('pointerup', end);
    target.addEventListener('pointercancel', end);
  };
  return (
    <aside ref={ref} className="hx-appmain__float" data-state={state} aria-label={title}>
      <div className="hx-appmain__float-head" onPointerDown={(e) => grab(e, 'move')}>
        <span className="hx-appmain__float-title">{title}</span>
      </div>
      <div className="hx-appmain__float-body">{children}</div>
      {FLOAT_EDGES.map((edge) => (
        <span
          key={edge}
          className="hx-appmain__float-size"
          data-edge={edge}
          aria-hidden="true"
          onPointerDown={(e) => grab(e, edge)}
        />
      ))}
    </aside>
  );
};

export interface IAppMainProps {
  title: string;
  /** The app's glyph before its name; absent on a document's page. */
  icon?: string;
  /** The chat's title, for the column and the window; null without a chat. */
  chatTitle: string | null;
  /** Under the app's name: which chat the app works in (the switch of Sinopia's chat head). */
  chatSwitch?: ReactNode;
  /** «Поделиться»: the link to the app for the team; absent on a document's page. */
  onShare?: () => void;
  placement: TPlacement;
  float: IFloat | null;
  onPlacement: (placement: TPlacement, section: ISection) => void;
  onFloat: (float: IFloat) => void;
  menu: readonly IAppMenuItem[];
  /** Before the title, at the head of the header: the switch between looking and editing. */
  lead?: ReactNode;
  /** The app's view. */
  stage: ReactNode;
  /** The chat (feed and field); null: no chat beside this app. */
  chat: ReactNode;
  /** The chat's field alone, for the foot. */
  island: ReactNode;
  /** The inspector under the view, when it is open. */
  inspector: ReactNode;
}

const AppMain = (props: IAppMainProps) => {
  const { placement, chat } = props;
  const bodyRef = useRef<HTMLDivElement>(null);
  const [live, setLive] = useState<IFloat | null>(null);
  const hasChat = chat !== null;
  const col = usePresentFlag(hasChat && placement === PLACEMENT.left, PLACE_EXIT_MS);
  const win = usePresentFlag(hasChat && placement === PLACEMENT.float, PLACE_EXIT_MS);
  const isl = usePresentFlag(hasChat && placement === PLACEMENT.island, PLACE_EXIT_MS);

  const size = useCallback((): ISection => {
    const box = bodyRef.current?.getBoundingClientRect();
    return { width: box?.width ?? window.innerWidth, height: box?.height ?? window.innerHeight };
  }, []);
  // The body's size for rendering: measured after layout and kept up as it changes (a ref is not read in render).
  const [section, setSection] = useState<ISection>(() => ({
    width: window.innerWidth,
    height: window.innerHeight,
  }));
  useLayoutEffect(() => {
    const el = bodyRef.current;
    if (!el) return;
    const measure = () => setSection(size());
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [size]);
  const floatNow = props.float ?? firstFloat(section);
  const shownFloat = live ?? floatNow;

  return (
    <div className="hx-appmain" data-placement={placement}>
      <header className="hx-appmain__bar">
        {props.lead}
        {props.icon && (
          <span className="hx-appmain__tile" aria-hidden="true">
            <Icon name={props.icon} />
          </span>
        )}
        <div className="hx-appmain__titles">
          <h1 className="hx-appmain__title">{props.title}</h1>
          {props.chatSwitch}
        </div>
        <span className="hx-spacer" />
        {hasChat && <PlacementSwitch value={placement} onChange={(p) => props.onPlacement(p, size())} />}
        {props.onShare && (
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={t('next.appmain.share')}
            onClick={props.onShare}
          >
            <Icon name="share" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.appmain.share')}
            </span>
          </button>
        )}
        <AppMenu items={props.menu} />
      </header>
      <div className="hx-appmain__body" ref={bodyRef}>
        {col.mounted && (
          <aside
            className="hx-appmain__col"
            data-state={col.state}
            aria-label={props.chatTitle ?? props.title}
          >
            {chat}
          </aside>
        )}
        <main className="hx-appmain__stage">
          {/* The field sits at the foot of the view, above the inspector when that is open. */}
          <div className="hx-appmain__view">
            {props.stage}
            {isl.mounted && (
              <div className="hx-appmain__island" data-state={isl.state}>
                {props.island}
              </div>
            )}
          </div>
          {props.inspector}
          {win.mounted && (
            <FloatWindow
              title={props.chatTitle ?? props.title}
              float={shownFloat}
              section={section}
              state={win.state}
              onFloat={props.onFloat}
              onLive={setLive}
            >
              {chat}
            </FloatWindow>
          )}
        </main>
      </div>
    </div>
  );
};

export default AppMain;
