// The notice of the board: it says what was done (and «Отменить» takes it back) or why a change did not go through. It
// slides in from the top with transform and opacity, and goes away on its own with the same ease-out.
import { useCallback, useContext, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import { useClosing } from '../../../org/ui/closing';
import { closingAttr, Icon, LayerCtx } from '../../ui';

const SHOW_MS = 4800;
const OUT_MS = 180;

export interface ToastState {
  text: string;
  error: boolean;
  undo: (() => void) | null;
  n: number;
}

export const useToast = () => {
  const [toast, setToast] = useState<ToastState | null>(null);
  const seq = useRef(0);
  const timer = useRef<number | undefined>(undefined);
  const put = useCallback((text: string, error: boolean, undo: (() => void) | null) => {
    window.clearTimeout(timer.current);
    seq.current += 1;
    setToast({ text, error, undo, n: seq.current });
    timer.current = window.setTimeout(() => setToast(null), SHOW_MS);
  }, []);
  const say = useCallback((text: string, undo?: () => void) => put(text, false, undo ?? null), [put]);
  const fail = useCallback((text: string) => put(text, true, null), [put]);
  const hide = useCallback(() => {
    window.clearTimeout(timer.current);
    setToast(null);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return { toast, say, fail, hide };
};

export const Toast = ({ toast, onClose }: { toast: ToastState | null; onClose: () => void }) => {
  const layer = useContext(LayerCtx);
  const { mounted, closing } = useClosing(toast !== null, OUT_MS);
  const [last, setLast] = useState<ToastState | null>(null);
  if (toast !== null && last !== toast) setLast(toast);
  const shown = toast ?? last;
  if (!mounted || !shown) return null;
  const el = (
    <div
      className="ev-toast"
      data-closing={closingAttr(closing)}
      data-error={shown.error ? '1' : undefined}
      role="status"
      key={shown.n}
    >
      <Icon i={shown.error ? 'circle-alert' : 'check'} size="sm" />
      <span>{shown.text}</span>
      {shown.undo && (
        <button
          type="button"
          onClick={() => {
            shown.undo?.();
            onClose();
          }}
        >
          Отменить
        </button>
      )}
    </div>
  );
  return layer ? createPortal(el, layer) : el;
};
