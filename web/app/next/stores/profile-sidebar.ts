// The person's sidebar as the cluster keeps it (features/sidebar/profile-layout.ts): read with the chat list,
// written by a change made on the revision the page holds. A document changed meanwhile (another tab, another device)
// is not overwritten: the page takes the newer one and makes its change on it again; after a few such turns the change
// is refused with words, never written over the other.
import { createStore, useStore } from '@/next/core/store';
import { gatewayPath } from '@/next/core/net/machine';
import { getPage, sendBody } from '@/next/core/net/http';
import { EMPTY_PROFILE } from '@/next/features/sidebar/profile-layout';
import { parseCentral, type ProfileSidebar } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

const TIMEOUT_MS = 15_000;
const PATH = '/history/sidebar';
const TRIES = 3;

export type ProfileSidebarView =
  | { k: 'loading' }
  /** The cluster keeps no sidebar for this person (its copy is off): each machine's own stands. */
  | { k: 'off' }
  | { k: 'ready'; rev: number; doc: ProfileSidebar };

const LOADING: ProfileSidebarView = { k: 'loading' };
export const profileSidebarStore = createStore<ProfileSidebarView>(LOADING);
export const useProfileSidebar = (): ProfileSidebarView => useStore(profileSidebarStore);

/** A newer revision than the page holds replaces it; an older answer (one that crossed a write) is dropped. */
const adopt = (rev: number, doc: ProfileSidebar | null): void => {
  const have = profileSidebarStore.get();
  if (have.k === 'ready' && have.rev > rev) return;
  profileSidebarStore.set({ k: 'ready', rev, doc: doc ?? EMPTY_PROFILE });
};

export async function refreshProfileSidebar(): Promise<void> {
  const res = await getPage(gatewayPath(PATH), parseCentral.sidebar, TIMEOUT_MS);
  if (!('ok' in res)) {
    // A gateway older than the person's sidebar has no such page.
    if ('r' in res && res.r === 'failed' && res.code === 'http' && res.status === 404)
      profileSidebarStore.set({ k: 'off' });
    return;
  }
  if (!res.value.enabled) {
    profileSidebarStore.set({ k: 'off' });
    return;
  }
  adopt(res.value.rev, res.value.doc);
}

let queue: Promise<unknown> = Promise.resolve();

async function attempt(
  edit: (doc: ProfileSidebar) => ProfileSidebar | null,
  tries: number,
): Promise<boolean> {
  const have = profileSidebarStore.get();
  if (have.k !== 'ready') throw new Error(t('next.profileSidebar.notReady'));
  const next = edit(have.doc);
  if (next === null) throw new Error(t('next.refuse.not-found'));
  if (next === have.doc) return false;
  const res = await sendBody(
    'PUT',
    gatewayPath(PATH),
    { baseRev: have.rev, doc: next },
    parseCentral.sidebarWrite,
    TIMEOUT_MS,
  );
  if (!('ok' in res)) throw new Error(t('next.profileSidebar.failed'));
  if (res.value.conflict) {
    adopt(res.value.rev, res.value.doc ?? null);
    if (tries <= 1) throw new Error(t('next.profileSidebar.conflict'));
    return attempt(edit, tries - 1);
  }
  adopt(res.value.rev, next);
  return true;
}

/**
 * A change of the person's sidebar, made on the document the page holds and written on its revision; changes go one
 * after another. Resolves whether anything was written; rejects with words when it could not be.
 */
export function editProfileSidebar(edit: (doc: ProfileSidebar) => ProfileSidebar | null): Promise<boolean> {
  const run = queue.then(() => attempt(edit, TRIES));
  queue = run.catch(() => undefined);
  return run;
}

/** A new session starts from nothing. */
export const resetProfileSidebar = (): void => profileSidebarStore.set(LOADING);
