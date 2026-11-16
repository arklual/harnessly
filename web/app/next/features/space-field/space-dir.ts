// The chat of the boards and the documents works in a folder of its own on the machine: not the folder of the
// person's last chat, whose CLAUDE.md and project memory are another project's. The machine names and makes it
// (cmd/runtime/space_dir.go); a machine without the route answers no folder, and the chat starts as before.
import { useEffect, useState } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

/** The folder asked of each machine: undefined while asked, null when the machine names none. */
const known = new Map<MachineId, string | null>();

/** The spaces' folder of the machine; null when it has none (an older machine). */
export const spaceDirOf = async (m: MachineId): Promise<string | null> => {
  const was = known.get(m);
  if (was !== undefined) return was;
  const dir = await runtimeJson<{ dir?: unknown }>(m, platformPath('/spaces/dir')).then(
    (r) => (typeof r.dir === 'string' && r.dir.startsWith('/') ? r.dir : null),
    () => null,
  );
  known.set(m, dir);
  return dir;
};

/**
 * The folder a space's chat starts in: the machine's spaces' folder, or `fallback` on a machine without one. Undefined
 * while it is asked: the chat does not start in another project's folder meanwhile.
 */
export const useSpaceDir = (
  m: MachineId | null,
  on: boolean,
  fallback: string | null,
): string | null | undefined => {
  const [dir, setDir] = useState<string | null | undefined>(() => (m ? known.get(m) : undefined));
  useEffect(() => {
    if (!on || m === null) return;
    let live = true;
    void spaceDirOf(m).then((d) => {
      if (live) setDir(d);
    });
    return () => {
      live = false;
    };
  }, [m, on]);
  if (!on) return fallback;
  if (dir === undefined) return undefined;
  return dir ?? fallback;
};

/** Forget what was asked (tests). */
export const forgetSpaceDirs = (): void => known.clear();
