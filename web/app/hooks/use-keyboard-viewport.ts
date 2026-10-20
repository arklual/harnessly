import { useEffect } from 'react';

/** Taller than any browser toolbar change: less than this is the address bar collapsing, not a keyboard. */
const KEYBOARD_MIN_PX = 120;

let users = 0;

/** The field shrinks as the keyboard opens; the line being typed has to stay in sight. */
const revealCaret = () => {
  const field = document.activeElement;
  if (!(field instanceof HTMLElement) || !field.isContentEditable) return;
  const selection = window.getSelection();
  if (!selection?.rangeCount) return;
  const caret = selection.getRangeAt(0).getBoundingClientRect();
  if (!caret.height) return;
  const box = field.getBoundingClientRect();
  if (caret.bottom > box.bottom) field.scrollTop += caret.bottom - box.bottom + 4;
  else if (caret.top < box.top) field.scrollTop -= box.top - caret.top + 4;
};
let detach: (() => void) | null = null;

const attach = () => {
  const viewport = window.visualViewport;
  if (!viewport) return () => {};
  const root = document.documentElement;
  const sync = () => {
    root.style.setProperty('--tw-visual-height', `${Math.round(viewport.height)}px`);
    if (window.innerHeight - viewport.height > KEYBOARD_MIN_PX) root.dataset.keyboardOpen = '';
    else delete root.dataset.keyboardOpen;
    requestAnimationFrame(revealCaret);
  };
  sync();
  viewport.addEventListener('resize', sync);
  return () => {
    viewport.removeEventListener('resize', sync);
    root.style.removeProperty('--tw-visual-height');
    delete root.dataset.keyboardOpen;
  };
};

/**
 * On a phone the on-screen keyboard only shrinks the visual viewport: the page keeps its height. The composer
 * reads `--tw-visual-height` and `html[data-keyboard-open]` so its text grows into what is left above the keys
 * instead of pushing the conversation off screen. Shared by every composer on the page.
 */
const useKeyboardViewport = (enabled: boolean) => {
  useEffect(() => {
    if (!enabled) return;
    users += 1;
    if (users === 1) detach = attach();
    return () => {
      users -= 1;
      if (users === 0) {
        detach?.();
        detach = null;
      }
    };
  }, [enabled]);
};

export default useKeyboardViewport;
