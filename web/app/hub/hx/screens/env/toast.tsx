// A small notice that a change did not go through (the screen already took it back). It slides in with transform and
// opacity only and goes away on its own.
import { type ReactNode, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import { useClosing } from '../../../org/ui/closing';
import { closingAttr, Icon, LayerCtx } from '../../ui';

const SHOW_MS = 5000;
const OUT_MS = 200;

export const useToast = () => {
  const [text, setText] = useState<string | null>(null);
  const [n, setN] = useState(0);
  const timer = useRef<number | undefined>(undefined);
  const show = useCallback((t: string) => {
    window.clearTimeout(timer.current);
    setText(t);
    setN((x) => x + 1);
    timer.current = window.setTimeout(() => setText(null), SHOW_MS);
  }, []);
  const hide = useCallback(() => {
    window.clearTimeout(timer.current);
    setText(null);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return { text, n, show, hide };
};

export const Toast = ({ text, id, onClose }: { text: string | null; id: number; onClose: () => void }) => {
  const layer = useContext(LayerCtx);
  const { mounted, closing } = useClosing(text !== null, OUT_MS);
  // Keep the last text on screen while it slides out.
  const [last, setLast] = useState<ReactNode>(null);
  if (text !== null && last !== text) setLast(text);
  if (!mounted) return null;
  const el = (
    <div className="env-toast" data-closing={closingAttr(closing)} role="status" key={id}>
      <Icon i="circle-alert" size="sm" />
      <span>{text ?? last}</span>
      <button type="button" className="env-toast__x" onClick={onClose} aria-label="Закрыть">
        <Icon i="x" size="sm" />
      </button>
    </div>
  );
  return layer ? createPortal(el, layer) : el;
};
