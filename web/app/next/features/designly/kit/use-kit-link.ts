// The kit a project is made from, read once per project; null while it is read, when it has none, or when
// the machine cannot say.
import { useEffect, useState } from 'react';
import { fetchKitLink, type IKitLink } from '@/next/features/designly/kit/kit-api';
import type { MachineId } from '@/next/protocol/v2';

export const useKitLink = (machine: MachineId, project: string): IKitLink | null => {
  const [link, setLink] = useState<{ of: string; link: IKitLink | null } | null>(null);
  useEffect(() => {
    const ac = new AbortController();
    fetchKitLink(machine, project, ac.signal).then(
      (found) => setLink({ of: project, link: found }),
      () => {
        if (!ac.signal.aborted) setLink({ of: project, link: null });
      },
    );
    return () => ac.abort();
  }, [machine, project]);
  return link?.of === project ? link.link : null;
};

/** The kit node a board shows, by the board's path: the first one the machine found there. */
export const boardNode = (link: IKitLink | null, board: string | null): string | null => {
  if (!link || !board) return null;
  const use = link.boards.find((b) => b.path === board);
  return use?.nodes[0] ?? null;
};
