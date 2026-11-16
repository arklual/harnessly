// The people of this page, from the machine's list (PersonSettings.people) and this browser's own name
// (stores/whoami.ts).
import { useMemo } from 'react';
import { peopleView, type IPeopleView } from '@/next/features/people/people';
import { meStore, useMe } from '@/next/stores/me';
import { useWhoami, whoamiSnapshot } from '@/next/stores/whoami';

const NO_PEOPLE: readonly string[] = [];

/** The machine's people as the page knows them now. */
export const usePeopleList = (): readonly string[] => {
  const me = useMe();
  return me.k === 'ready' ? me.me.settings.people : NO_PEOPLE;
};

/** The view while it is on, null while it is off. */
export const usePeopleView = (): IPeopleView | null => {
  const people = usePeopleList();
  const me = useWhoami();
  return useMemo(() => peopleView(me, people), [me, people]);
};

/** The person this page writes as, outside React (commands/run.ts): null while the view is off. */
export const pagePerson = (): string | null => {
  const st = meStore.get();
  return peopleView(whoamiSnapshot(), st.k === 'ready' ? st.me.settings.people : NO_PEOPLE)?.me ?? null;
};
