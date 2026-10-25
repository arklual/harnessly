// Whether the page is in its dark theme: the `dark` class on <html>, which the theme switch (and the galleries) set.
import { useSyncExternalStore } from 'react';

const subscribe = (notify: () => void) => {
  const observer = new MutationObserver(notify);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
  return () => observer.disconnect();
};

const read = () => document.documentElement.classList.contains('dark');

const useDark = (): boolean => useSyncExternalStore(subscribe, read, () => false);

export default useDark;
