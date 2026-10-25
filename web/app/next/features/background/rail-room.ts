// Whether the right column has room beside the chat. It stands at the top right of the stage and must not come near the
// column of the chat (its widest width, centred in what the pane leaves of the stage): the room is counted from the
// widths, not read off the screen, so it does not flicker while the pane or the sidebar slides. The left sidebar needs
// no rule of its own: it takes its width off the stage, and the room is what the stage has left. Whether the right pane
// is open is read here too (the stage carries it), because it always takes the column away.
import { useEffect, useState } from 'react';

/** The air kept between the chat's column and the right column: the column never covers more than this takes. */
export const RAIL_GAP = 12;
/** Where the column stands from the stage's edge: at rest, and beside the pane (styles/background.css, pane.css). */
const EDGE = 16;
const EDGE_BESIDE_PANE = 28;
/** The room the pane keeps from the feed: its width and the gap to the feed (styles/pane.css). */
const PANE_GAP = 12;
const PHONE_QUERY = '(max-width: 900px)';

export interface IRailGeometry {
  /** The stage's width, and the pane's (0 with no pane). */
  stage: number;
  pane: number;
  /** The chat column's widest width and the side padding of the feed. */
  feedMax: number;
  feedPad: number;
  /** The right column's own width. */
  rail: number;
}

/** True when the column stands clear of the chat's column by RAIL_GAP. */
export const railFits = ({ stage, pane, feedMax, feedPad, rail }: IRailGeometry): boolean => {
  const area = stage - (pane > 0 ? pane + PANE_GAP : 0);
  const column = Math.min(feedMax, Math.max(0, area - 2 * feedPad));
  const columnRight = area - (area - column) / 2;
  const railLeft = stage - (pane > 0 ? pane + EDGE_BESIDE_PANE : EDGE) - rail;
  return columnRight + RAIL_GAP <= railLeft;
};

const px = (value: string, fallback: number): number => {
  const n = Number.parseFloat(value);
  return Number.isFinite(n) ? n : fallback;
};

const measure = (rail: HTMLElement, stage: HTMLElement): boolean => {
  const feed = stage.querySelector<HTMLElement>('.hx-feed');
  const inner = stage.querySelector<HTMLElement>('.hx-feed__inner');
  const pane = stage.querySelector<HTMLElement>('.hx-pane:not([data-state="closing"])');
  const feedPad = feed ? px(getComputedStyle(feed).paddingLeft, 32) : 32;
  return railFits({
    stage: stage.clientWidth,
    pane: pane ? pane.offsetWidth : 0,
    feedMax: inner ? px(getComputedStyle(inner).maxWidth, 760) : 760,
    feedPad,
    rail: rail.offsetWidth,
  });
};

export interface IRailRoom {
  /** The column fits beside the chat's column, the pane (when open) counted. */
  room: boolean;
  /** The right pane is open on the stage. */
  pane: boolean;
}

const NO_ROOM: IRailRoom = { room: false, pane: false };

/**
 * What the screen allows the column on its own: it fits beside the chat and the window is not a phone, and whether the
 * right pane is open. Measured when the stage or the pane changes (the observers' calls are joined into one read a
 * frame), not by polling.
 */
export const useRailRoom = (rail: HTMLElement | null): IRailRoom => {
  const [state, setState] = useState<IRailRoom>(NO_ROOM);

  useEffect(() => {
    const stage = rail?.offsetParent instanceof HTMLElement ? rail.offsetParent : null;
    if (!rail || !stage) return;
    const phone = window.matchMedia(PHONE_QUERY);
    const update = () => {
      const room = !phone.matches && measure(rail, stage);
      const pane = stage.dataset.pane === 'true';
      setState((now) => (now.room === room && now.pane === pane ? now : { room, pane }));
    };
    update();
    let frame = 0;
    const soon = () => {
      if (frame === 0)
        frame = requestAnimationFrame(() => {
          frame = 0;
          update();
        });
    };
    const resize = new ResizeObserver(soon);
    resize.observe(stage);
    // The pane comes and goes inside the stage, and its width is a custom property on the stage's style.
    const mutate = new MutationObserver(soon);
    mutate.observe(stage, { attributes: true, attributeFilter: ['style', 'data-pane'], childList: true });
    phone.addEventListener('change', update);
    return () => {
      cancelAnimationFrame(frame);
      resize.disconnect();
      mutate.disconnect();
      phone.removeEventListener('change', update);
    };
  }, [rail]);

  return rail === null ? NO_ROOM : state;
};
