// A 3D scene open over the machine: the project of the kind `scene3d` from the library, its state from the scene's route
// (kept in stores/scene3d.ts and read again every 3 s while the page is visible, the runtime's `rev` telling a change; a
// model still building shows up the same way) and the scene's shell. designly-site.tsx renders it for such a project.
import { useEffect, useState } from 'react';
import Scene3dView, { type IScene3dViewProps } from '@/next/features/designly/scene3d/scene3d-view';
import { fetchScene3d } from '@/next/features/designly/scene3d/api';
import { setScene3d, useScene3d } from '@/next/stores/scene3d';

export type IDesignlyScene3dProps = Omit<IScene3dViewProps, 'state' | 'stateError' | 'canPin'> & {
  viewing?: boolean;
};

/** The scene is read again this often while its page is visible, ms. */
const POLL_MS = 3000;

const useScene3dState = (machine: IScene3dViewProps['machine'], id: string): string | null => {
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let abort: AbortController | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let live = true;
    const read = () => {
      if (timer) clearTimeout(timer);
      timer = null;
      abort?.abort();
      const now = new AbortController();
      abort = now;
      fetchScene3d(machine, id, now.signal).then(
        (state) => {
          if (!live) return;
          setScene3d(id, state);
          setError(null);
          schedule();
        },
        (failure: Error) => {
          if (!live || now.signal.aborted) return;
          setError(failure.message);
          schedule();
        },
      );
    };
    const schedule = () => {
      if (live && document.visibilityState === 'visible') timer = setTimeout(read, POLL_MS);
    };
    const visible = () => {
      if (document.visibilityState === 'visible') read();
      else if (timer) clearTimeout(timer);
    };
    read();
    document.addEventListener('visibilitychange', visible);
    return () => {
      live = false;
      abort?.abort();
      if (timer) clearTimeout(timer);
      document.removeEventListener('visibilitychange', visible);
    };
  }, [machine, id]);
  return error;
};

const DesignlyScene3d = ({ viewing, ...props }: IDesignlyScene3dProps) => {
  const error = useScene3dState(props.machine, props.id);
  const state = useScene3d(props.id);
  return <Scene3dView {...props} state={state} stateError={error} canPin={viewing !== true} />;
};

export default DesignlyScene3d;
