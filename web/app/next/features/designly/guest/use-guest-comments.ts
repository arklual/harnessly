// The comments of a guest link, read now and again every 20 s while the page is there.
import { useEffect, useState } from 'react';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';
import { fetchComments, type IGuestComment } from '@/next/features/designly/guest/guest-comments';

const COMMENTS_MS = 20_000;

export const useComments = (config: IGuestConfig) => {
  const [comments, setComments] = useState<IGuestComment[]>([]);
  const on = config.rights === 'comment' && config.commentsUrl !== '';
  useEffect(() => {
    if (!on) return;
    const abort = new AbortController();
    const read = () =>
      void fetchComments(config.commentsUrl, abort.signal).then((list) => {
        if (list) setComments(list);
      });
    read();
    const timer = window.setInterval(read, COMMENTS_MS);
    return () => {
      abort.abort();
      window.clearInterval(timer);
    };
  }, [on, config.commentsUrl]);
  return [comments, setComments] as const;
};
