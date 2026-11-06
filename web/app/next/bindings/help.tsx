// The documentation on the machine: the sections with their pages and the page the address names, in the person's
// language, and the search over every page. The address is the page (/help/<section>/<page>#<heading>); a fresh page
// starts at its top, a heading is scrolled to once it is drawn.
import { useCallback, useEffect, useState } from 'react';
import {
  HelpError,
  loadGuidePage,
  loadLibrary,
  pictureUrl,
  searchGuide,
  type IGuideSection,
} from '@/next/features/help/api';
import HelpView, { type TSearch } from '@/next/features/help/help-view';
import { placeOf } from '@/next/features/help/model';
import { pageClock } from '@/next/core/clock';
import { locale } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';

interface IAsked<T> {
  load: unknown;
  attempt: number;
  data: T | null;
  error: HelpError | null;
}

/** One ask of the machine, asked again with `retry`; the answer is null (and no error) from the moment `load` changes. */
const useAsk = <T,>(load: (() => Promise<T>) | null) => {
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<IAsked<T> | null>(null);
  useEffect(() => {
    if (!load) return;
    let live = true;
    load().then(
      (data) => live && setState({ load, attempt, data, error: null }),
      (error: unknown) =>
        live &&
        setState({
          load,
          attempt,
          data: null,
          error: error instanceof HelpError ? error : new HelpError(String(error), false),
        }),
    );
    return () => {
      live = false;
    };
  }, [load, attempt]);
  const fresh = state?.load === load && state.attempt === attempt ? state : null;
  return { data: fresh?.data ?? null, error: fresh?.error ?? null, retry: () => setAttempt((n) => n + 1) };
};

/** How long typing pauses before the search is asked. */
const SEARCH_PAUSE_MS = 200;

/** The search of the words typed, asked once typing pauses; a newer query drops the older answer. */
const useSearch = (
  machine: MachineId,
  lang: ReturnType<typeof locale>,
  query: string,
  attempt: number,
): TSearch => {
  const [state, setState] = useState<{ query: string; attempt: number; search: TSearch } | null>(null);
  const words = query.trim();
  useEffect(() => {
    if (words === '') return;
    const ctl = new AbortController();
    const clock = pageClock();
    const timer = clock.setTimeout(() => {
      searchGuide(machine, lang, words, ctl.signal).then(
        (hits) => setState({ query: words, attempt, search: { kind: 'hits', hits } }),
        (error: unknown) => {
          if (ctl.signal.aborted) return;
          setState({
            query: words,
            attempt,
            search: {
              kind: 'failed',
              error: error instanceof HelpError ? error : new HelpError(String(error), false),
            },
          });
        },
      );
    }, SEARCH_PAUSE_MS);
    return () => {
      clock.clearTimeout(timer);
      ctl.abort();
    };
  }, [machine, lang, words, attempt]);
  if (words === '') return { kind: 'idle' };
  return state?.query === words && state.attempt === attempt ? state.search : { kind: 'loading' };
};

const HelpPage = ({
  machine,
  section,
  page,
  anchor,
  onOpenDrawer,
}: {
  machine: MachineId;
  section: string | null;
  page: string | null;
  anchor: string | null;
  onOpenDrawer: () => void;
}) => {
  const lang = locale();
  const library = useAsk<IGuideSection[]>(useCallback(() => loadLibrary(machine, lang), [machine, lang]));
  const sections = library.data;
  const place = sections ? placeOf(sections, section, page) : null;
  const sectionId = place?.section.id ?? null;
  const pageName = place?.page.name ?? null;
  const loadOne = useCallback(
    () => loadGuidePage(machine, lang, sectionId ?? '', pageName ?? ''),
    [machine, lang, sectionId, pageName],
  );
  const one = useAsk<string>(sectionId && pageName ? loadOne : null);
  const markdown = place ? one.data : null;
  const [query, setQuery] = useState('');
  const [searchAttempt, setSearchAttempt] = useState(0);
  const search = useSearch(machine, lang, query, searchAttempt);

  // A heading named by the address, once the page is drawn; a page without one starts at its top.
  useEffect(() => {
    if (markdown === null) return;
    const target = anchor ? document.getElementById(anchor) : null;
    if (target) target.scrollIntoView({ block: 'start' });
    else document.querySelector('.hx-help')?.scrollTo({ top: 0 });
  }, [markdown, sectionId, pageName, anchor]);

  const pictureSrc = useCallback(
    (src: string) => pictureUrl(machine, lang, sectionId ?? '', pageName ?? '', src),
    [machine, lang, sectionId, pageName],
  );
  const onOpen = useCallback((to: string, name: string, heading?: string) => {
    goTo({ kind: ROUTE.help, section: to, page: name, ...(heading ? { anchor: heading } : {}) });
  }, []);

  let onRetry = one.retry;
  if (library.error) onRetry = library.retry;
  else if (search.kind === 'failed') onRetry = () => setSearchAttempt((n) => n + 1);

  return (
    <HelpView
      sections={sections}
      sectionsError={library.error}
      place={place}
      markdown={markdown}
      pageError={place ? one.error : null}
      query={query}
      search={search}
      onQuery={setQuery}
      onOpen={onOpen}
      pictureSrc={pictureSrc}
      onRetry={onRetry}
      onOpenDrawer={onOpenDrawer}
    />
  );
};

export default HelpPage;
