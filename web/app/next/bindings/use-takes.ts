// «Записи»: the person's recordings, those this browser keeps and those in the cluster, newest first. A take
// of this browser plays from its own bytes; one only in the cluster plays from there while its audio is kept (30 days),
// after that only its text is left. The cluster's list is read when the list is opened.
import { useEffect, useState } from 'react';
import { useAll } from '@/next/core/store';
import {
  cloudAudioUrl,
  deleteCloudTake,
  listCloudTakes,
  type ICloudTake,
} from '@/next/features/composer/voice-cloud';
import { dropTake, loadTakes, takesStore, type VoiceTake } from '@/next/features/composer/voice-takes';
import type { ITakeRow } from '@/next/features/composer/takes-list';
import { personKey, useMe } from '@/next/stores/me';

/** Object URLs of the browser's own recordings, made once per recording. */
const urls = new Map<string, { audio: Blob; url: string }>();

const urlOf = (take: VoiceTake) => {
  const known = urls.get(take.id);
  if (known?.audio === take.audio) return known.url;
  if (known) URL.revokeObjectURL(known.url);
  const url = URL.createObjectURL(take.audio);
  urls.set(take.id, { audio: take.audio, url });
  return url;
};

const sameRows = (a: VoiceTake[], b: VoiceTake[]) =>
  a.length === b.length && a.every((take, i) => take === b[i]);

/** `open`: the list is on screen; the cluster's part is read each time it opens. */
export const useTakes = (open: boolean) => {
  const me = useMe();
  const person = me.k === 'ready' ? personKey(me.me) : null;
  const local = useAll(
    takesStore,
    (s) => [...s.values()].filter((take) => take.person === person && take.state !== 'live'),
    sameRows,
  );
  /** null: not read yet, or the cluster keeps none (standalone). */
  const [cloud, setCloud] = useState<ICloudTake[] | null>(null);
  /** The cluster answered with its list (true), or there is none to ask (false); null until it is known. */
  const [shared, setShared] = useState<boolean | null>(null);
  useEffect(() => {
    if (!open) return;
    void loadTakes();
    let alive = true;
    void listCloudTakes().then((list) => {
      if (!alive) return;
      setCloud(list ?? []);
      setShared(list !== null);
    });
    return () => {
      alive = false;
    };
  }, [open]);

  const mine = new Map(local.map((take) => [take.id, take]));
  const rows: ITakeRow[] = [
    ...local.map((take) => ({
      id: take.id,
      at: take.at,
      ms: take.ms,
      mime: take.mime,
      text: take.text,
      failed: take.state === 'failed',
      audio: take.audio.size > 0 ? urlOf(take) : null,
    })),
    ...(cloud ?? [])
      .filter((take) => !mine.has(take.id))
      .map((take) => ({
        id: take.id,
        at: take.at,
        ms: take.ms,
        mime: take.mime,
        text: take.text,
        failed: take.state === 'failed',
        audio: take.audio ? cloudAudioUrl(take.id) : null,
      })),
  ].sort((a, b) => b.at - a.at);

  const remove = async (id: string) => {
    dropTake(id);
    setCloud((list) => list && list.filter((take) => take.id !== id));
    await deleteCloudTake(id);
  };

  return { rows, loading: open && cloud === null && local.length === 0, remove, shared };
};

/** «Записи» of «+»: the list is read while its pane is shown; a tap puts a take's text into the field. */
export const useTakesMenu = (onInsert: (text: string) => void) => {
  const [shown, setShown] = useState(false);
  const { rows, loading } = useTakes(shown);
  return { rows, loading, onShow: setShown, onInsert };
};
