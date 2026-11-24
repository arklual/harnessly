import { copyWriter, offlineBase } from '@/next/offline/client';
import { boundedBlob } from '@/next/offline/file';
import { limit, owner, shelf, type Copy } from '@/next/offline/storage';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { toast } from './sdk/toast';
import { deckExportHref } from './deck/api';
import { runDeckExport } from './deck/exports';

async function save(machine: MachineId, artifact: string, title: string, revision?: number) {
  const write = copyWriter();
  if (!write) throw new Error(t('next.offline.account'));
  if (revision !== undefined) {
    const person = owner(offlineBase);
    if (!person) return;
    const db = await shelf(offlineBase, person);
    try {
      const req = db
        .transaction('copies')
        .objectStore('copies')
        .get(JSON.stringify(['design', machine, artifact]));
      const copy = await new Promise<Copy | undefined>((resolve, reject) => {
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });
      if (copy?.revision !== undefined && copy.revision >= revision) return;
    } finally {
      db.close();
    }
  } else toast(t('next.offline.preparing'));
  const job = await runDeckExport(machine, artifact, 'html', { quiet: true });
  const response = await fetch(deckExportHref(machine, artifact, job.id), { redirect: 'error' });
  if (!response.ok || !response.headers.get('content-disposition')?.toLowerCase().includes('attachment'))
    throw new Error(t('next.deck.menu.failed'));
  if (Number(response.headers.get('content-length')) > limit) throw new Error(t('next.offline.large'));
  await write(['design', machine, artifact], title, 'html', await boundedBlob(response), revision);
  if (revision === undefined) toast(t('next.offline.saved'));
}
export function preloadDesign(machine: MachineId, artifact: string, title: string, revision: number) {
  if (
    !navigator.onLine ||
    document.hidden ||
    !navigator.locks ||
    !copyWriter() ||
    (navigator as Navigator & { connection?: { saveData: boolean } }).connection?.saveData
  )
    return;
  void navigator.locks
    .request(
      'hx-offline-design:' + JSON.stringify([offlineBase, machine, artifact]),
      { ifAvailable: true },
      async (lock) => {
        if (lock) await save(machine, artifact, title, revision);
      },
    )
    .catch(() => undefined);
}
export function offlineDownload(machine: MachineId, artifact: string, title: string) {
  return [
    {
      id: 'offline-copy',
      icon: 'download',
      label: t('next.offline.save'),
      pick: () =>
        void save(machine, artifact, title).catch((e) => toast(String(e.message ?? e), { tone: 'error' })),
    },
    {
      id: 'offline-library',
      icon: 'files',
      label: t('next.offline.library'),
      pick: () => window.open(offlineBase + 'offline.html', '_blank', 'noopener'),
    },
  ];
}
