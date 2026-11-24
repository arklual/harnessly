import { appBasePath } from '@/platform/config';
import { owner, selectOwner, saveCopy, type Copy } from './storage';

export const offlineBase = appBasePath().replace(/\/$/, '') + '/';
let confirmed: string | null = null;
export function confirmOfflineOwner(person: string | null) {
  confirmed = person;
  selectOwner(offlineBase, person);
}
export function copyWriter() {
  const person = owner(offlineBase);
  if (!person || person !== confirmed) return null;
  return (key: string[], title: string, kind: Copy['kind'], body: Blob, revision?: number) =>
    saveCopy(offlineBase, person, { id: JSON.stringify(key), title, kind, body, revision, at: Date.now() });
}
