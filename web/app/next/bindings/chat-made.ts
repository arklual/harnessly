// «Сделано в этом чате» off the runtime: GET …/made asked when the tools home opens and again a second after the chat's
// state moves (the same pace as the plan), again with a growing pause while the runtime still reads the transcript or
// could not answer. The pictures only decide whether the Gallery has content. The result is what the block draws and what says which tools
// the chat has content for.
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from 'react';
import { loadMade } from '@/next/commands/pages';
import { ARTIFACT_KIND, LOOK, type IArtifactItem } from '@/next/features/artifacts/model';
import { retryMetas, useArtifactMetas } from '@/next/features/artifacts/meta-store';
import type { IMadeCard } from '@/next/features/pane/made-block';
import {
  aboutOf,
  cachedMade,
  dedupeMade,
  fromOf,
  iconOf,
  kindWord,
  madeKey,
  mergeImages,
  rememberMade,
  taskEntries,
  whereOf,
  type MadeTarget,
} from '@/next/features/pane/made-model';
import type { Conversation, MadeItem } from '@/next/protocol/v2';
import { useLinkRestored } from '@/next/stores/connection';
import { useShots, usePictures } from '@/next/stores/conversation';
import { hasFeature } from '@/next/stores/machines';

/** The pause after the chat's state moved before the list is asked again. */
export const MADE_DEBOUNCE_MS = 1000;
/** The pause before asking again of an unfinished or failed answer: it doubles to the longest. */
export const MADE_RETRY_MS = 2000;
export const MADE_RETRY_MAX_MS = 16_000;

export const retryPause = (tries: number): number =>
  Math.min(MADE_RETRY_MS * 2 ** Math.max(0, tries - 1), MADE_RETRY_MAX_MS);

/** A title the runtime did not answer (a slow phone network) is asked for again this many times, with growing pauses. */
export const TITLE_TRIES = 5;
/** The names still being read are shown together after this long at the latest, ms. */
export const TITLES_BATCH_MS = 900;

export interface IChatMade {
  cards: IMadeCard[] | null;
  /** The pictures, documents and boards, designs the chat holds; null until known. */
  holds: { pictures: boolean; documents: boolean; designs: boolean } | null;
  /** Where a card, or a task inside one, opens. */
  open: (card: IMadeCard) => MadeTarget | undefined;
}

const metaItem = (item: MadeItem): IArtifactItem => ({
  url: item.ref,
  kind: item.kind === 'board' ? ARTIFACT_KIND.board : ARTIFACT_KIND.doc,
  key: null,
  label: null,
  fallback: kindWord(item),
  where: whereOf(item),
  github: false,
  readable: true,
  at: item.at,
});

/**
 * What the chat made. `on` is whether the tools home is on screen: nothing is asked while it is not. A runtime that
 * does not announce the list has made nothing to show, and a failure with nothing known yet is quiet (the block just
 * is not there) while it is asked again.
 */
export const useChatMade = (conv: Conversation, on: boolean): IChatMade => {
  const announced = hasFeature(conv.machineId, 'made');
  const [, bump] = useReducer((n: number) => n + 1, 0);
  const [failedFor, setFailedFor] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const asked = useRef<string | null>(null);
  const tries = useRef(0);
  const restored = useLinkRestored(conv.machineId);
  const moved = `${conv.state.k}|${conv.state.since}|${conv.lastActivityAt}|${attempt}|${restored}`;

  useEffect(() => {
    if (!on || !announced) return;
    let live = true;
    let retry = 0;
    // The first ask of a chat is at once; the later ones follow the state with a pause.
    const wait = asked.current === conv.id ? MADE_DEBOUNCE_MS : 0;
    const timer = window.setTimeout(() => {
      asked.current = conv.id;
      void loadMade(conv.machineId, conv.id).then((res) => {
        if (!live) return;
        if ('ok' in res) {
          rememberMade(conv.id, res.value);
          setFailedFor(null);
          bump();
          tries.current = res.value.complete ? 0 : tries.current + 1;
          if (!res.value.complete)
            retry = window.setTimeout(() => setAttempt((n) => n + 1), retryPause(tries.current));
          return;
        }
        setFailedFor(conv.id);
        tries.current += 1;
        retry = window.setTimeout(() => setAttempt((n) => n + 1), retryPause(tries.current));
      });
    }, wait);
    return () => {
      live = false;
      window.clearTimeout(timer);
      window.clearTimeout(retry);
    };
  }, [on, announced, conv.machineId, conv.id, moved]);

  const made = announced ? cachedMade(conv.id) : null;
  const items = useMemo<MadeItem[] | null>(() => {
    if (!announced) return [];
    if (made) return dedupeMade(made.items);
    return failedFor === conv.id ? [] : null;
  }, [announced, made, failedFor, conv.id]);

  const shots = useShots(conv.id);
  const pictureNames = usePictures(conv.id);
  const images = useMemo(
    () => (shots === null || pictureNames === null ? null : mergeImages(shots, pictureNames)),
    [shots, pictureNames],
  );

  const metaItems = useMemo(
    () =>
      (items ?? []).filter((x) => (x.kind === 'doc' || x.kind === 'board') && x.title === '').map(metaItem),
    [items],
  );
  const metas = useArtifactMetas(conv.machineId, metaItems);
  // A title the runtime did not answer is asked again, a few times, as the phone's network allows.
  const titleTries = useRef(0);
  const [round, again] = useReducer((n: number) => n + 1, 0);
  const unanswered = metas.failed;
  useEffect(() => {
    if (unanswered === 0) {
      titleTries.current = 0;
      return;
    }
    if (titleTries.current >= TITLE_TRIES) return;
    const timer = window.setTimeout(
      () => {
        titleTries.current += 1;
        retryMetas(conv.machineId, metaItems);
        again();
      },
      retryPause(titleTries.current + 1),
    );
    return () => window.clearTimeout(timer);
  }, [unanswered, metaItems, conv.machineId, round]);
  // The names come together: what the browser kept shows at once, and what has to be read is held (a bar in its slot)
  // until every answer is in or a short time has passed, then it all shows in one update — not one name at a time.
  // Only the names that had to be read wait; the ones the browser already kept (known when the block first stood) show at once.
  const [kept] = useState<ReadonlySet<string>>(
    () => new Set(metaItems.filter((x) => metas.of(x).meta !== null).map((x) => x.url)),
  );
  const [late, setLate] = useState(false);
  const holding = metas.waiting > 0 && !late;
  useEffect(() => {
    if (metas.waiting === 0) return;
    const timer = window.setTimeout(() => setLate(true), TITLES_BATCH_MS);
    return () => window.clearTimeout(timer);
  }, [metas.waiting]);
  /** The name of a thing: its own, the one read for its link, a bar while it is read, the kind's word when it is not to be had. */
  const titleOf = (item: MadeItem): { title: string; pending: boolean; known: boolean } => {
    if (item.title !== '') return { title: item.title, pending: false, known: true };
    const found = metaItems.find((x) => x.url === item.ref);
    if (!found) return { title: kindWord(item), pending: false, known: false };
    const read = metas.of(found);
    if (read.meta && (!holding || kept.has(found.url)))
      return { title: read.meta.title, pending: false, known: true };
    const waiting = read.look === LOOK.wait || (read.meta !== null && holding);
    return { title: kindWord(item), pending: waiting, known: false };
  };

  // The tasks are told by the tracker and come with their own names: they join the cards by the time they were touched.
  const entries = useMemo(() => (items === null ? null : taskEntries(items)), [items]);
  const drawn =
    items === null || entries === null
      ? null
      : [
          // Merge requests have their block under a finished task, not a card here.
          ...items
            .filter((x) => x.kind !== 'task' && x.kind !== 'mr')
            .map((x) => {
              const name = titleOf(x);
              return {
                at: x.at,
                card: {
                  id: madeKey(x),
                  icon: iconOf(x),
                  title: name.title,
                  pending: name.pending,
                  // Two pages with no name between them are told apart by where they live.
                  // What a subchat or a subagent made says so: the chat inherits it.
                  about: [name.known ? aboutOf(x) : whereOf(x), fromOf(x)].filter(Boolean).join(' · '),
                } satisfies IMadeCard,
              };
            }),
          ...entries,
        ]
          .sort((a, b) => b.at - a.at)
          .map((x) => x.card);
  // The same cards while their words did not change: the block keeps what it shows by identity.
  const sig = drawn === null ? null : JSON.stringify(drawn);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- `sig` is `drawn` in words
  const cards = useMemo<IMadeCard[] | null>(() => drawn, [sig]);
  const holds =
    items === null || images === null
      ? null
      : {
          pictures: images.length > 0,
          documents: items.some((x) => x.kind === 'doc' || x.kind === 'board'),
          designs: items.some((x) => x.kind === 'design'),
        };
  const open = useCallback(
    (card: IMadeCard): MadeTarget | undefined =>
      entries?.flatMap((x) => x.targets).find(([id]) => id === card.id)?.[1] ??
      items?.find((x) => x.kind !== 'task' && madeKey(x) === card.id),
    [items, entries],
  );
  return { cards, holds, open };
};
