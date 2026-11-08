// SPDX-License-Identifier: Apache-2.0
// Manual-mode hint bubble (open state is owned by the caller), reduced from the design system Hint: the
// placement is the same pure function, the bubble is portaled to a shared host, enter and exit are CSS keyframes.
import {
  type ReactElement,
  type ReactNode,
  cloneElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';

import { classNames } from './cx';
import { type HintBox, type HintPlacementDirection, computeHintPlacement } from './hint-placement';

export type VizHintDirection = HintPlacementDirection;

export interface VizHintProps {
  readonly open: boolean;
  readonly onOpenChange?: (open: boolean) => void;
  readonly content: ReactNode;
  readonly direction?: VizHintDirection | readonly VizHintDirection[];
  /** Gap between the anchor and the bubble, px. */
  readonly offset?: number;
  /** The pointer may enter the bubble; false makes it click-through. */
  readonly interactive?: boolean;
  readonly role?: 'tooltip' | 'dialog';
  readonly 'aria-label'?: string;
  readonly className?: string;
  /** Exactly one element: the anchor. It receives a ref. */
  readonly children: ReactElement;
}

/** Upper bound of an exit animation; a missed animationend never keeps the bubble mounted. */
const EXIT_FALLBACK_MS = 400;

let host: HTMLElement | null = null;

/** One host per document, itself a .viz-root so the bubble reads the chart tokens outside the card. */
function portalHost(): HTMLElement {
  if (host && host.isConnected) return host;
  host = document.createElement('div');
  host.className = 'viz-root viz-portal';
  document.body.appendChild(host);
  return host;
}

function readViewport(): HintBox {
  const root = document.documentElement;
  return {
    top: 0,
    left: 0,
    width: root.clientWidth || window.innerWidth,
    height: root.clientHeight || window.innerHeight,
  };
}

const coarse = () =>
  typeof window.matchMedia === 'function' && window.matchMedia('(pointer: coarse)').matches;

/** Keeps a closing bubble mounted until its CSS exit animation ends. */
function usePresence(open: boolean, node: HTMLElement | null): boolean {
  const [present, setPresent] = useState(open);
  if (open && !present) setPresent(true);
  useLayoutEffect(() => {
    if (open || !present) return;
    const animation = node ? getComputedStyle(node).animationName : '';
    if (!node || !animation || animation === 'none') {
      setPresent(false);
      return;
    }
    const finish = (event: Event) => event.target === node && setPresent(false);
    node.addEventListener('animationend', finish);
    node.addEventListener('animationcancel', finish);
    const timer = window.setTimeout(() => setPresent(false), EXIT_FALLBACK_MS);
    return () => {
      node.removeEventListener('animationend', finish);
      node.removeEventListener('animationcancel', finish);
      window.clearTimeout(timer);
    };
  }, [open, present, node]);
  return open || present;
}

export function Hint({
  open,
  onOpenChange,
  content,
  direction = 'bottom-start',
  offset = 8,
  interactive = true,
  role = 'tooltip',
  'aria-label': ariaLabel,
  className,
  children,
}: VizHintProps) {
  const id = useId();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [bubble, setBubble] = useState<HTMLDivElement | null>(null);
  const empty = content === null || content === undefined || content === '' || content === false;
  const visible = open && !empty;
  const present = usePresence(visible, bubble);
  const update = useRef<(() => void) | null>(null);
  const latest = useRef({ direction, offset });
  latest.current = { direction, offset };
  const directionKey = typeof direction === 'string' ? direction : direction.join(' ');

  useEffect(() => {
    if (!visible) return;
    const onKey = (event: KeyboardEvent) => event.key === 'Escape' && onOpenChange?.(false);
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [visible, onOpenChange]);

  const place = useCallback(() => {
    if (!anchor || !bubble) return;
    if (anchor.getClientRects().length === 0) {
      bubble.removeAttribute('data-positioned');
      return;
    }
    const width = bubble.offsetWidth;
    const height = bubble.offsetHeight;
    if (width === 0 || height === 0) return;
    const rect = anchor.getBoundingClientRect();
    const placement = computeHintPlacement({
      anchor: { top: rect.top, left: rect.left, width: rect.width, height: rect.height },
      bubble: { width, height },
      viewport: readViewport(),
      direction: latest.current.direction,
      offset: coarse() ? Math.max(latest.current.offset, 16) : latest.current.offset,
      centered: true,
      rtl: getComputedStyle(anchor).direction === 'rtl',
    });
    bubble.style.setProperty('--viz-hint-x', `${placement.x}px`);
    bubble.style.setProperty('--viz-hint-y', `${placement.y}px`);
    bubble.style.setProperty('--viz-hint-beak-x', `${placement.beakX}%`);
    bubble.style.setProperty('--viz-hint-beak-y', `${placement.beakY}%`);
    bubble.style.setProperty('--viz-hint-beak-rotate', `${placement.beakRotate}deg`);
    bubble.setAttribute('data-direction', placement.direction);
    bubble.setAttribute('data-positioned', '');
  }, [anchor, bubble]);
  update.current = place;

  // The anchor and the content move with every render of the owner: measure again after each one.
  useLayoutEffect(() => {
    update.current?.();
  });

  useLayoutEffect(() => {
    if (!present || !bubble || !anchor) return;
    const again = () => update.current?.();
    window.addEventListener('resize', again);
    window.addEventListener('scroll', again, true);
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(again) : null;
    observer?.observe(bubble);
    return () => {
      window.removeEventListener('resize', again);
      window.removeEventListener('scroll', again, true);
      observer?.disconnect();
    };
  }, [present, bubble, anchor, directionKey]);

  const trigger = cloneElement(children as ReactElement<Record<string, unknown>>, {
    ref: setAnchor,
    'aria-describedby': visible && role === 'tooltip' ? id : undefined,
  });
  const node = present ? (
    <div
      ref={setBubble}
      id={id}
      role={role}
      aria-label={role === 'dialog' ? ariaLabel : undefined}
      className={classNames('viz-bubble', className)}
      data-direction={typeof direction === 'string' ? direction : direction[0]}
      data-interactive={interactive ? 'true' : 'false'}
      data-leaving={visible ? undefined : ''}
    >
      {content}
    </div>
  ) : null;
  return (
    <>
      {trigger}
      {node ? createPortal(node, portalHost()) : null}
    </>
  );
}
