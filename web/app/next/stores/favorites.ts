// How the pinned conversations stand, from the convs channel: its snapshot carries the layout when there is one, and
// every change of it comes whole as favorites_upsert. Which chats are favorites is their own `pinned`; this is the
// groups and the order (see Favorites in the protocol).

import { createKeyedStore, useKey } from '@/next/core/store';
import type { Favorites, MachineId } from '@/next/protocol/v2';

export const EMPTY_FAVORITES: Favorites = { groups: [], loose: [] };

export const favoritesStore = createKeyedStore<MachineId, Favorites>();

/** A machine's layout; empty until its snapshot says otherwise. */
export const useFavorites = (machine: MachineId | null): Favorites =>
  useKey(favoritesStore, machine) ?? EMPTY_FAVORITES;
