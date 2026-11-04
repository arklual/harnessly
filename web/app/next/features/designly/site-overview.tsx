// «Все экраны»: the page on every screen at once, as Polypane and Responsively show it. The screens stand in a row by
// group (phones, tablets, computers) on the canvas, which pans and zooms as a single page does (use-canvas-zoom.ts moves
// .hx-ovw). The wheel over a screen scrolls its page and the others follow it to the same place; chips over the canvas
// choose the groups shown, kept per browser.
//
// The frames are the slot's pages of another origin: they are spoken to through the slot's bridge (bridge-core.ts),
// `tk:scroll-sync` asks a page to tell its scroll (`tk:doc-scroll`) and `tk:scroll-to` moves it. Only the screen under the
// pointer leads: the others' answers to being moved are not passed on again.
import { useEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import { GUEST_ORIGIN, fromFrame } from '@/next/features/designly/bridge-origin';
import {
  OVERVIEW_GROUPS,
  syncedScroll,
  type IOverviewCard,
  type TOverviewGroup,
} from '@/next/features/designly/devices';
import { readBridgeMessage } from '@/next/features/designly/page-bridge';

/** The groups «Все экраны» shows; per browser. */
const GROUPS_KEY = 'hx-overview-groups';

const readGroups = (): TOverviewGroup[] => {
  try {
    const kept = (localStorage.getItem(GROUPS_KEY) ?? '').split(',');
    const groups = OVERVIEW_GROUPS.filter((g) => kept.includes(g));
    return groups.length > 0 ? groups : [...OVERVIEW_GROUPS];
  } catch {
    return [...OVERVIEW_GROUPS];
  }
};

const writeGroups = (groups: readonly TOverviewGroup[]) => {
  try {
    localStorage.setItem(GROUPS_KEY, groups.join(','));
  } catch {
    // Private mode or full storage: the choice holds for this page.
  }
};

/** The groups shown and the toggle of one; the last group shown stays. */
export const useOverviewGroups = () => {
  const [groups, setGroups] = useState<readonly TOverviewGroup[]>(readGroups);
  const toggle = (group: TOverviewGroup) => {
    const next = OVERVIEW_GROUPS.filter((g) => (g === group) !== groups.includes(g));
    if (next.length === 0) return;
    setGroups(next);
    writeGroups(next);
  };
  return { groups, toggle };
};

interface IOverviewProps {
  cards: readonly IOverviewCard[];
  /** Room at the bottom for the field or a guest's list of comments (devices.ts overviewBox counts it too). */
  room: boolean;
  /** The page at an address the slot lets in without a ticket; null until the slot has let the first frame in. */
  src: string | null;
  /** A guest's board has an opaque origin: it is addressed as `*` and believed for being the frame's window. */
  guest: boolean;
  groups: readonly TOverviewGroup[];
  onGroup: (group: TOverviewGroup) => void;
  onPick: (id: string) => void;
  /** What a page tells the canvas: Space held over it, a drag with the middle button. */
  onSpace: (down: boolean) => void;
  onPan: (dx: number, dy: number, drag: boolean) => void;
}

export const Overview = ({
  cards,
  room,
  src,
  guest,
  groups,
  onGroup,
  onPick,
  onSpace,
  onPan,
}: IOverviewProps) => {
  const rowRef = useRef<HTMLDivElement>(null);
  /** The frames by device, and those whose page has loaded (a frame on its way is a blank page of this origin). */
  const frames = useRef(new Map<string, HTMLIFrameElement>());
  const loaded = useRef(new Set<string>());
  /** The screen under the pointer: its scroll is the one the others follow. */
  const leader = useRef<string | null>(null);
  const scrollY = useRef(0);
  const origin = src === null || guest ? GUEST_ORIGIN : new URL(src, window.location.href).origin;
  const latest = useRef({ onSpace, onPan });
  useEffect(() => {
    latest.current = { onSpace, onPan };
  });

  const post = (id: string, message: Record<string, unknown>) => {
    if (loaded.current.has(id)) frames.current.get(id)?.contentWindow?.postMessage(message, origin);
  };
  const latestPost = useRef(post);
  useEffect(() => {
    latestPost.current = post;
  });

  useEffect(() => {
    if (src === null) return;
    const onMessage = (event: MessageEvent) => {
      const found = [...frames.current].find(([, frame]) => fromFrame(event, frame.contentWindow, origin));
      if (!found) return;
      const [id, frame] = found;
      const message = readBridgeMessage(event.data);
      if (message?.k === 'doc-scroll') {
        if (leader.current !== id) return;
        scrollY.current = syncedScroll(message.y);
        for (const other of frames.current.keys())
          if (other !== id) latestPost.current(other, { type: 'tk:scroll-to', y: scrollY.current });
        return;
      }
      if (message?.k === 'wheel') {
        // A pinch or ⌘-wheel over a screen: the canvas takes it as its own wheel at the same spot.
        const at = frame.getBoundingClientRect();
        const k = frame.offsetWidth > 0 ? at.width / frame.offsetWidth : 1;
        rowRef.current?.dispatchEvent(
          new WheelEvent('wheel', {
            bubbles: true,
            cancelable: true,
            ctrlKey: true,
            deltaY: message.dy,
            clientX: at.left + message.x * k,
            clientY: at.top + message.y * k,
          }),
        );
        return;
      }
      if (message?.k === 'space') latest.current.onSpace(message.down);
      else if (message?.k === 'pan') latest.current.onPan(message.dx, message.dy, message.drag);
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [src, origin]);

  return (
    <>
      <div className="hx-ovwchips" role="group" aria-label={t('next.site.devices.overview')}>
        {OVERVIEW_GROUPS.map((group) => {
          const on = groups.includes(group);
          return (
            <button
              key={group}
              type="button"
              className="hx-ovwchip"
              aria-pressed={on}
              aria-disabled={on && groups.length === 1}
              onClick={() => onGroup(group)}
            >
              {t(`next.site.deviceGroups.${group}`)}
            </button>
          );
        })}
      </div>
      <div className="hx-ovw" data-room={room} ref={rowRef}>
        {cards.map(({ device, w, h, scale, lead }) => {
          const name = t(`next.site.devices.${device.id}`);
          const size = `${w}×${h}`;
          return (
            <div
              key={device.id}
              className="hx-ovw__card"
              data-lead={lead}
              ref={(el) => {
                el?.style.setProperty('--w', `${w}px`);
                el?.style.setProperty('--h', `${h}px`);
                el?.style.setProperty('--s', scale.toFixed(4));
              }}
            >
              <button
                type="button"
                className="hx-ovw__label"
                aria-label={`${name} ${size}: ${t('next.site.overviewOpen')}`}
                title={t('next.site.overviewOpen')}
                onClick={() => onPick(device.id)}
              >
                <span className="hx-ovw__name">{name}</span>
                <span className="hx-ovw__size">{size}</span>
              </button>
              <div
                className="hx-ovw__screen"
                data-round={w < 1000}
                onPointerEnter={() => {
                  leader.current = device.id;
                }}
              >
                <div className="hx-ovw__scroll">
                  {src !== null && (
                    <iframe
                      key={src}
                      className="hx-ovw__frame"
                      src={src}
                      title={`${name} ${size}`}
                      ref={(frame) => {
                        if (frame === null) return;
                        frames.current.set(device.id, frame);
                        return () => {
                          frames.current.delete(device.id);
                          loaded.current.delete(device.id);
                        };
                      }}
                      onLoad={() => {
                        loaded.current.add(device.id);
                        // The board draws the library's boards bar unless it is told the host draws its own; the page
                        // is on a canvas and scrolls with the others, from where they are.
                        post(device.id, { type: 'tk:tweaks-host', on: true, open: false });
                        post(device.id, { type: 'tk:canvas', on: true });
                        post(device.id, { type: 'tk:scroll-sync', on: true });
                        if (scrollY.current > 0)
                          post(device.id, { type: 'tk:scroll-to', y: scrollY.current });
                      }}
                    />
                  )}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
};
