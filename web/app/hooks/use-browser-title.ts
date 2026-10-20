import { useEffect } from 'react';
import useTabStore, { selectGlobalStatus } from '@/hooks/use-tab-store';

/** The tab title, with the number of agents waiting for the person in front of it. */
export default function useBrowserTitle(title: string): void {
  const waiting = useTabStore((s) => selectGlobalStatus(s.tabs).attentionCount);
  useEffect(() => {
    document.title = waiting > 0 ? `(${waiting}) ${title}` : title;
  }, [waiting, title]);
}
