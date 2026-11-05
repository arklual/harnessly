// The browser tab's talk with the bridge of its page: whether it can pick, the element picked, the page's own address
// as it moves on, and the offer page's «Открыть» for a redirect to another site. Only messages of the tab's frame from
// the slot's origin count.
import { useEffect, useEffectEvent, useRef, useState, type RefObject } from 'react';
import {
  readFrameMessage,
  type IFrameActed,
  type IFramePicked,
  type TFrameMessage,
} from '@/next/features/pane/frame-bridge';

/** An agent's action for the page's frame, as the slot's bridge reads `tk:act`. */
export interface IFrameAct {
  id: string;
  op: string;
  ref?: string;
  text?: string;
  key?: string;
  value?: string;
  dy?: number;
  ms?: number;
  submit?: boolean;
  /** A wait for a video or audio: ended, playing or paused. */
  event?: string;
  tool?: string;
  input?: string;
  /** The site the person let the agent act on: the live browser checks the page is still there. */
  host?: string;
}

type TWaiter = (acted: IFrameActed | null) => void;

/** A message of the frame, by its kind. */
export type TFrameMessageOf<K extends TFrameMessage['k']> = Extract<TFrameMessage, { k: K }>;

interface IKindWaiter {
  kind: TFrameMessage['k'];
  heard: (message: TFrameMessage) => void;
}

interface IFrameBridgeArgs {
  frame: RefObject<HTMLIFrameElement | null>;
  /** The slot's origin; null before the slot is open. */
  origin: string | null;
  /** The person pressed «Открыть» on the offer of a redirect to another site. */
  onOpen: (url: string) => void;
  /** The site redirected to another one (a sign-in, say): the tab may follow it by itself; true when it does. */
  onRedirect: (url: string) => boolean;
}

export const useFrameBridge = ({ frame, origin, onOpen, onRedirect }: IFrameBridgeArgs) => {
  // Null until the page's bridge says hello: a page without one cannot pick.
  const [picking, setPicking] = useState<boolean | null>(null);
  const [picked, setPicked] = useState<IFramePicked | null>(null);
  const [url, setUrl] = useState<string | null>(null);
  /** How many times a page's bridge said hello: each new document says it again. */
  const [hellos, setHellos] = useState(0);
  const opened = useEffectEvent((url: string) => onOpen(url));
  const redirected = useEffectEvent((url: string): boolean => onRedirect(url));
  // The actions waiting for the frame's answer, and those waiting for the next page's hello.
  const acts = useRef(new Map<string, TWaiter>());
  const waitHello = useRef(new Set<() => void>());
  const waitKind = useRef(new Set<IKindWaiter>());
  // The nonce of the document that last said hello; a ref too, so a login reads it as it is now.
  const [nonce, setNonce] = useState<string | null>(null);
  const nonceNow = useRef<string | null>(null);
  const setNonceNow = (next: string | null) => {
    nonceNow.current = next;
    setNonce(next);
  };

  useEffect(() => {
    if (origin === null) return;
    const onMessage = (event: MessageEvent) => {
      const known = frame.current?.contentWindow ?? null;
      if (known === null || event.source !== known || event.origin !== origin) return;
      const message = readFrameMessage(event.data);
      if (!message) return;
      for (const waiter of [...waitKind.current]) if (waiter.kind === message.k) waiter.heard(message);
      if (message.k === 'hello') {
        setNonceNow(message.nonce);
        setPicking((on) => on ?? false);
        setUrl(message.url);
        setHellos((n) => n + 1);
        for (const heard of [...waitHello.current]) heard();
      } else if (message.k === 'acted') {
        const waiter = acts.current.get(message.acted.id);
        acts.current.delete(message.acted.id);
        waiter?.(message.acted);
      } else if (message.k === 'nav') setUrl(message.url);
      else if (message.k === 'pick') setPicking(message.on);
      else if (message.k === 'picked') {
        setPicking(false);
        setPicked(message.picked);
      } else if (message.k === 'redirect') {
        // The page's bridge holds back a page that leaves by script until it hears whether the tab goes there itself.
        const follow = redirected(message.url);
        frame.current?.contentWindow?.postMessage(
          { type: 'tk:redirect-answer', url: message.url, follow },
          origin,
        );
      } else if (message.k === 'open') opened(message.url);
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [frame, origin]);

  const post = (on: boolean) => {
    if (origin !== null) frame.current?.contentWindow?.postMessage({ type: 'tk:pick', on }, origin);
  };

  return {
    picking,
    picked,
    /** The page's address in the slot, as its bridge last said; null before it did. */
    url,
    togglePick: () => {
      if (picking === null) return;
      const next = !picking;
      setPicking(next);
      if (next) setPicked(null);
      post(next);
    },
    endPick: () => {
      setPicked(null);
      if (picking) {
        setPicking(false);
        post(false);
      }
    },
    /** A new document in the frame: its bridge says hello again; what the old one was doing is lost with it. */
    onFrameLoad: () => {
      setPicking(null);
      setPicked(null);
      setNonceNow(null);
      for (const waiter of [...acts.current.values()]) waiter(null);
      acts.current.clear();
    },
    /** Has the frame do an action; null when it did not answer in time or its document went. */
    act: (message: IFrameAct, timeoutMs: number): Promise<IFrameActed | null> =>
      new Promise((resolve) => {
        const target = frame.current?.contentWindow ?? null;
        if (origin === null || target === null) {
          resolve(null);
          return;
        }
        const timer = window.setTimeout(() => {
          acts.current.delete(message.id);
          resolve(null);
        }, timeoutMs);
        acts.current.set(message.id, (acted) => {
          window.clearTimeout(timer);
          resolve(acted);
        });
        target.postMessage({ type: 'tk:act', ...message }, origin);
      }),
    /** Resolves on the next page's hello (true), or false after timeoutMs. */
    nextHello: (timeoutMs: number): Promise<boolean> =>
      new Promise((resolve) => {
        const heard = () => {
          window.clearTimeout(timer);
          waitHello.current.delete(heard);
          resolve(true);
        };
        const timer = window.setTimeout(() => {
          waitHello.current.delete(heard);
          resolve(false);
        }, timeoutMs);
        waitHello.current.add(heard);
      }),
    hellos,
    /** The nonce of the document that last said hello; null before it did, or when it sent none. */
    nonce,
    /** The same, read now: for a flow that runs across several messages. */
    nonceNow: (): string | null => nonceNow.current,
    /** The next message of this kind from the frame, or null after timeoutMs. */
    nextMessage: <K extends TFrameMessage['k']>(
      kind: K,
      timeoutMs: number,
    ): Promise<TFrameMessageOf<K> | null> =>
      new Promise((resolve) => {
        const waiter: IKindWaiter = {
          kind,
          heard: (message) => {
            window.clearTimeout(timer);
            waitKind.current.delete(waiter);
            resolve(message as TFrameMessageOf<K>);
          },
        };
        const timer = window.setTimeout(() => {
          waitKind.current.delete(waiter);
          resolve(null);
        }, timeoutMs);
        waitKind.current.add(waiter);
      }),
    /** Tells the page's bridge something that needs no answer. */
    post: (message: Record<string, unknown>) => {
      if (origin !== null) frame.current?.contentWindow?.postMessage(message, origin);
    },
    /** The page's own back, inside its frame. */
    back: () => {
      if (origin !== null)
        frame.current?.contentWindow?.postMessage({ type: 'tk:go', action: 'back' }, origin);
    },
  };
};
