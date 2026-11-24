export interface Copy {
  id: string;
  title: string;
  kind: 'text' | 'html' | 'media' | 'model' | 'docx' | 'pptx';
  body: Blob;
  at: number;
  revision?: number;
}

export const limit = 32 * 1024 * 1024;
export const ownerKey = (base: string) => 'hx-offline-person:' + base;
export function owner(base: string) {
  try {
    return localStorage.getItem(ownerKey(base));
  } catch {
    return null;
  }
}
export function selectOwner(base: string, person: string | null) {
  try {
    if (person) localStorage.setItem(ownerKey(base), person);
    else localStorage.removeItem(ownerKey(base));
  } catch {
    /* Storage is optional. */
  }
}

export async function shelf(base: string, person: string) {
  if (owner(base) !== person) throw new Error('Offline account changed');
  const db = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open('hx-offline:' + JSON.stringify([base, person]), 1);
    request.onupgradeneeded = () => {
      request.result.createObjectStore('copies', { keyPath: 'id' }).createIndex('at', 'at');
      request.result.createObjectStore('drafts');
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  if (owner(base) !== person) {
    db.close();
    throw new Error('Offline account changed');
  }
  return db;
}

export async function saveCopy(base: string, person: string, copy: Copy) {
  if (copy.body.size > limit) throw new Error('Offline copy exceeds 32 MiB');
  const db = await shelf(base, person);
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(['copies', 'drafts'], 'readwrite');
      tx.oncomplete = () => resolve();
      tx.onabort = () => reject(tx.error);
      const store = tx.objectStore('copies');
      store.put(copy);
      let count = 0,
        bytes = 0;
      const scan = store.index('at').openCursor(null, 'prev');
      scan.onsuccess = () => {
        const cursor = scan.result;
        if (!cursor) return;
        const size = (cursor.value as Copy).body.size;
        if (count < 64 && bytes + size <= limit * 4) {
          count++;
          bytes += size;
          cursor.continue();
          return;
        }
        const draft = tx.objectStore('drafts').getKey(cursor.primaryKey);
        draft.onsuccess = () => {
          if (draft.result === undefined) cursor.delete();
          cursor.continue();
        };
      };
    });
  } finally {
    db.close();
  }
}
