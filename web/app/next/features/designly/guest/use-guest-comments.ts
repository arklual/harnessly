// The comments of a guest link, read now and again every 20 s while the page is there. A link revoked or expired meanwhile
// stops the reading and leaves for the gateway's page about it.
import { useEffect, useState } from 'react';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';
import { fetchComments, type IGuestComment } from '@/next/features/designly/guest/guest-comments';
import { leaveGoneLink } from '@/next/features/designly/guest/guest-gone';

const COMMENTS_MS = 20_000;

export const useComments = (config: IGuestConfig) => {
  const [comments, setComments] = useState<IGuestComment[]>([]);
  const on = config.rights === 'comment' && config.commentsUrl !== '';
  const token = config.token;
  useEffect(() => {
    if (!on) return;
    const abort = new AbortController();
    const stop = () => {
      abort.abort();
      window.clearInterval(timer);
    };
    const read = () =>
      void fetchComments(config.commentsUrl, abort.signal).then((list) => {
        if (list === 'gone') {
          stop();
          leaveGoneLink(token);
        } else if (list) setComments(list);
      });
    const timer = window.setInterval(read, COMMENTS_MS);
    read();
    return stop;
  }, [on, config.commentsUrl, token]);
  return [comments, setComments] as const;
};
