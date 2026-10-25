// xterm over the runtime's terminal socket (README §39): the shared body of the agent's terminal and the own shells
// of the right pane. The socket comes back after a drop while the terminal is shown.
import { useEffect } from 'react';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal, type ITheme } from '@xterm/xterm';
import { isCopyKey, isPasteKey, isReservedKey, mappedBytes } from '@/next/features/terminal/keys';
import { skipIdleSelectionRedraws } from '@/next/features/terminal/selection-redraw';
import '@xterm/xterm/css/xterm.css';

const HEARTBEAT_MS = 30_000;
const RETRY_MS = 2000;

const FONT_SIZE = 12.5;
const LINE_HEIGHT = 1.2;
const SCROLLBACK = 5000;
/** A wheel notch glides over this long instead of jumping several rows; a trackpad moves by its own deltas. */
const SMOOTH_SCROLL_MS = 120;
/** The scrollbar's width: xterm draws it inside the screen and the fit leaves room for it. */
const SCROLLBAR_W = 6;

/** The terminal socket's frames (README §39): 0x00 keys, 0x01 output, 0x02 size, 0x03 heartbeat. */
const FRAME = { input: 0, output: 1, resize: 2, beat: 3 } as const;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

const frame = (kind: number, body: Uint8Array): Uint8Array<ArrayBuffer> => {
  const out = new Uint8Array(body.length + 1);
  out[0] = kind;
  out.set(body, 1);
  return out;
};

const resizeFrame = (cols: number, rows: number) => {
  const out = new Uint8Array(5);
  const view = new DataView(out.buffer);
  out[0] = FRAME.resize;
  view.setUint16(1, cols);
  view.setUint16(3, rows);
  return out;
};

const isMac = () => /Mac|iPhone|iPad/.test(navigator.platform);

/** xterm needs concrete colours: the terminal's tokens (--hx-term-*) read from the element it sits in. */
const terminalTheme = (el: HTMLElement): ITheme => {
  const style = getComputedStyle(el);
  const token = (name: string) => style.getPropertyValue(name).trim();
  return {
    background: token('--hx-term-bg'),
    foreground: token('--hx-term-fg'),
    cursor: token('--hx-term-cursor'),
    cursorAccent: token('--hx-term-bg'),
    selectionBackground: token('--hx-term-select'),
    // The ruler beside the scrollbar draws a hairline of its own, white by default.
    overviewRulerBorder: 'transparent',
    scrollbarSliderBackground: token('--hx-term-scrollbar'),
    scrollbarSliderHoverBackground: token('--hx-term-scrollbar-hover'),
    scrollbarSliderActiveBackground: token('--hx-term-scrollbar-hover'),
  };
};

/** An unlaid-out container (closed dock, hidden tab) has nothing to measure: FitAddon would make it 2×1. */
const canFit = (el: HTMLElement) => el.isConnected && el.clientWidth > 0 && el.clientHeight > 0;

/** One socket per open terminal; it comes back after a drop while the panel is open. */
export const useTerminalSocket = (
  open: boolean,
  url: string,
  host: React.RefObject<HTMLDivElement | null>,
) => {
  useEffect(() => {
    const el = host.current;
    if (!open || !el) return;
    const term = new Terminal({
      fontFamily: getComputedStyle(el).fontFamily,
      fontSize: FONT_SIZE,
      lineHeight: LINE_HEIGHT,
      cursorBlink: true,
      // The scrollbar's colours carry alpha, as the page's own scrollbars.
      allowTransparency: true,
      scrollback: SCROLLBACK,
      smoothScrollDuration: SMOOTH_SCROLL_MS,
      overviewRuler: { width: SCROLLBAR_W },
      theme: terminalTheme(el),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(el);
    skipIdleSelectionRedraws(term);
    if (canFit(el)) fit.fit();

    let socket: WebSocket | null = null;
    let beat = 0;
    let retry = 0;
    let closed = false;
    const send = (data: Uint8Array<ArrayBuffer>) => {
      if (socket?.readyState === WebSocket.OPEN) socket.send(data);
    };
    const type = (keys: string) => send(frame(FRAME.input, encoder.encode(keys)));
    const connect = () => {
      const ws = new WebSocket(`${url}&cols=${term.cols}&rows=${term.rows}`);
      ws.binaryType = 'arraybuffer';
      socket = ws;
      ws.onopen = () => {
        send(resizeFrame(term.cols, term.rows));
        beat = window.setInterval(() => send(new Uint8Array([FRAME.beat])), HEARTBEAT_MS);
      };
      ws.onmessage = (event) => {
        const data = new Uint8Array(event.data as ArrayBuffer);
        if (data[0] === FRAME.output) term.write(decoder.decode(data.subarray(1), { stream: true }));
      };
      ws.onclose = () => {
        window.clearInterval(beat);
        if (!closed) retry = window.setTimeout(connect, RETRY_MS);
      };
    };
    connect();
    const input = term.onData(type);

    // The keys are the terminal's while it has the focus: xterm hands over only what the page reserves for itself
    // (see keys.ts), and what xterm does not turn into bytes (the digits, Esc) never reaches the page's handlers.
    const mac = isMac();
    term.attachCustomKeyEventHandler((event) => {
      if (isReservedKey(event)) return false;
      if (event.type !== 'keydown') return true;
      if (isCopyKey(event)) {
        event.preventDefault();
        const selected = term.getSelection();
        if (selected) void navigator.clipboard?.writeText(selected).catch(() => undefined);
        return false;
      }
      // The browser pastes into xterm's field, and xterm sends the paste on.
      if (isPasteKey(event)) return false;
      const bytes = mappedBytes(event, mac);
      if (bytes === null) return true;
      event.preventDefault();
      type(bytes);
      return false;
    });
    const keepKeys = (event: KeyboardEvent) => {
      if (!isReservedKey(event)) event.stopPropagation();
    };
    el.addEventListener('keydown', keepKeys);

    // The size follows the box once per frame, and the session hears of it only when the grid changed: a dock
    // dragged by its grip or a window resized no longer sends a size for every pixel.
    // A layout change (the pane widened, a tab moved) can take the focus out from under the terminal without the user
    // leaving it: the terminal keeps wanting it until the focus goes to another element, and takes it back after a fit.
    let wantsFocus = false;
    // A control that changes the layout (marked data-keeps-term-focus) takes the focus on its press without taking the
    // terminal's claim to it: the terminal gets the focus back once the layout has settled.
    const keeper = (target: EventTarget | null) =>
      target instanceof Element && target.closest('[data-keeps-term-focus]') !== null;
    const onFocusOut = (event: FocusEvent) => {
      if (event.relatedTarget && !keeper(event.relatedTarget)) wantsFocus = false;
    };
    let settleFrame = 0;
    const onKeeperClick = (event: MouseEvent) => {
      if (!wantsFocus || !keeper(event.target)) return;
      window.cancelAnimationFrame(settleFrame);
      settleFrame = window.requestAnimationFrame(() => {
        settleFrame = window.requestAnimationFrame(() => {
          settleFrame = 0;
          if (wantsFocus && !el.contains(document.activeElement)) term.focus();
        });
      });
    };
    document.addEventListener('click', onKeeperClick);
    const onFocusIn = () => {
      wantsFocus = true;
    };
    const onPointerDown = () => {
      wantsFocus = true;
      term.focus();
    };
    el.addEventListener('focusout', onFocusOut);
    el.addEventListener('focusin', onFocusIn);
    el.addEventListener('pointerdown', onPointerDown);
    let frameId = 0;
    const observer = new ResizeObserver(() => {
      if (frameId) return;
      frameId = window.requestAnimationFrame(() => {
        frameId = 0;
        if (!canFit(el)) return;
        fit.fit();
        if (wantsFocus && !el.contains(document.activeElement)) term.focus();
      });
    });
    observer.observe(el);
    const resized = term.onResize(({ cols, rows }) => send(resizeFrame(cols, rows)));
    term.focus();
    return () => {
      closed = true;
      window.clearTimeout(retry);
      window.clearInterval(beat);
      window.cancelAnimationFrame(frameId);
      window.cancelAnimationFrame(settleFrame);
      document.removeEventListener('click', onKeeperClick);
      observer.disconnect();
      el.removeEventListener('keydown', keepKeys);
      el.removeEventListener('focusout', onFocusOut);
      el.removeEventListener('focusin', onFocusIn);
      el.removeEventListener('pointerdown', onPointerDown);
      input.dispose();
      resized.dispose();
      socket?.close();
      term.dispose();
    };
  }, [open, url, host]);
};
