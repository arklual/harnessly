import { useEffect, useMemo, useState } from 'react';
import { appFetch } from '@/lib/app-path';
import type { IViewableFile } from '@/lib/hl-artifacts';

/** Keeps only files the viewer can actually open, so a card never leads to "file not found". */
const useAvailableFiles = (files: IViewableFile[]): IViewableFile[] => {
  const key = files.map((file) => file.path).join('\n');
  const [available, setAvailable] = useState<{ key: string; paths: Set<string> } | null>(null);
  useEffect(() => {
    if (!key) return;
    let alive = true;
    appFetch('/api/platform/file-preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths: key.split('\n') }),
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((body: { available?: string[] } | null) => {
        if (alive && body?.available) setAvailable({ key, paths: new Set(body.available) });
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [key]);
  return useMemo(
    () => (available ? files.filter((file) => available.paths.has(file.path)) : []),
    [files, available],
  );
};

export default useAvailableFiles;
