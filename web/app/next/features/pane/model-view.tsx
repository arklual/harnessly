import { useEffect, useRef, useState } from 'react';
import { showModel } from '@/next/offline/model';
import { limit } from '@/next/offline/storage';
import { copyWriter } from '@/next/offline/client';
export default function ModelView({
  src,
  path,
  machine,
  conv,
}: {
  src: string;
  path: string;
  machine: string;
  conv: string;
}) {
  const ref = useRef<HTMLCanvasElement>(null);
  const [failure, setFailure] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    const canvas = ref.current!;
    const write = copyWriter();
    async function load() {
      const response = await fetch(src, { signal: controller.signal });
      if (!response.ok) throw new Error('HTTP ' + response.status);
      const body = await response.blob();
      if (controller.signal.aborted) return;
      await showModel(body, canvas, controller.signal);
      if (!controller.signal.aborted && !response.redirected && body.size <= limit)
        void write?.(['file', machine, conv, path], path.split('/').pop() ?? path, 'model', body).catch(
          () => undefined,
        );
    }
    void load().catch((e) => {
      if (!controller.signal.aborted) setFailure(String(e));
    });
    return () => controller.abort();
  }, [src, path, machine, conv]);
  return (
    <>
      {failure && <p role="alert">{failure}</p>}
      <canvas ref={ref} aria-label={path} style={{ width: '100%', height: '100%', minHeight: 240 }} />
    </>
  );
}
