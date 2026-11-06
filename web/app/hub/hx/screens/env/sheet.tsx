// The window of the board: it slides in from the right (a bottom sheet of a fixed height on a phone) with a head, an
// optional «Куда», a scrolling body and a foot. The hub's drawer has no foot or «Куда», so the frame is built here, and it
// closes through the same 200 ms animation. What stands in the head, the «Куда» row and the foot belongs to the page that
// is inside (the add flow, the form, the check), so each page puts it there through `<Slot>` while the frame stays put
// when the page changes: nothing flickers going from «Добавить» to a form.
import { createContext, type ReactNode, useContext, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import { useClosing } from '../../../org/ui/closing';
import { Btn, closingAttr, DRAWER_MS, Icon, LayerCtx } from '../../ui';

type SlotName = 'back' | 'title' | 'sub' | 'to' | 'foot';
type Slots = Partial<Record<SlotName, HTMLElement>>;
const SlotCtx = createContext<Slots>({});

/** Puts its children into a place of the frame (the head's title, «Куда», the foot…). */
export const Slot = ({ name, children }: { name: SlotName; children: ReactNode }) => {
  const el = useContext(SlotCtx)[name];
  return el ? createPortal(children, el) : null;
};

export const Sheet = ({
  open,
  label,
  onClose,
  children,
}: {
  open: boolean;
  label: string;
  onClose: () => void;
  children: ReactNode;
}) => {
  const layer = useContext(LayerCtx);
  const { mounted, closing } = useClosing(open, DRAWER_MS);
  const panel = useRef<HTMLElement>(null);
  const close = useRef(onClose);
  const [slots, setSlots] = useState<Slots>({});
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    if (!open) return;
    panel.current?.focus({ preventScroll: true });
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented) close.current();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);
  const slot = (name: SlotName) => (el: HTMLElement | null) => {
    setSlots((s) => (el && s[name] !== el ? { ...s, [name]: el } : s));
  };
  if (!mounted) return null;
  const c = closingAttr(closing);
  const el = (
    <>
      <div className="ad-scrim" data-closing={c} onClick={onClose} />
      <aside
        ref={panel}
        className="ad-sheet env-sh"
        data-closing={c}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
      >
        <div className="ad-sheet__head">
          <span ref={slot('back')} className="ev-sh__slot" />
          <div className="ev-sh__titles">
            <h3 ref={slot('title')} />
            <div ref={slot('sub')} />
          </div>
          <Btn variant="ghost" size="sm" shape="icon" onClick={onClose} aria-label="Закрыть">
            <Icon i="x" />
          </Btn>
        </div>
        <div ref={slot('to')} className="ev-to-slot" />
        <SlotCtx.Provider value={slots}>
          <div className="ad-sheet__body">{children}</div>
        </SlotCtx.Provider>
        <div ref={slot('foot')} className="ev-foot-slot" />
      </aside>
    </>
  );
  return layer ? createPortal(el, layer) : el;
};

export const Title = ({
  text,
  sub,
  onBack,
}: {
  text: string;
  sub?: { kind: string; initial: string; text: string } | null;
  onBack?: (() => void) | null;
}) => (
  <>
    <Slot name="title">{text}</Slot>
    <Slot name="sub">
      {sub && (
        <div className="ev-sh__sub">
          <span className="ad-dot" data-l={sub.kind}>
            {sub.initial}
          </span>
          {sub.text}
        </div>
      )}
    </Slot>
    <Slot name="back">
      {onBack && (
        <Btn
          variant="ghost"
          size="sm"
          shape="icon"
          className="ev-sh__back"
          onClick={onBack}
          aria-label="Назад"
        >
          <Icon i="arrow-left" />
        </Btn>
      )}
    </Slot>
  </>
);

export const Foot = ({ children }: { children: ReactNode }) => (
  <Slot name="foot">
    <div className="ev-sheet__foot">{children}</div>
  </Slot>
);
