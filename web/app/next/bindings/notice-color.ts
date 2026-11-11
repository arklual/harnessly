// The person's colour of what pops up (stores/me.ts noticeColor) on the page's root, where the --hx-toast-* tokens are
// chosen by it and the theme (styles/tokens.css): the notices and toasts are portalled out of the app's frame.
import { useEffect } from 'react';
import type { TNoticeColor } from '@/next/stores/me';

export const useNoticeColor = (color: TNoticeColor): void => {
  useEffect(() => {
    const root = document.documentElement;
    root.dataset.notices = color;
    return () => {
      delete root.dataset.notices;
    };
  }, [color]);
};
