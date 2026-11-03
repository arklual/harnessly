// The documentation on the machine: the list of pages and the page the address names, in the person's language. The
// address is the page (/help/<page>#<heading>); a fresh page starts at its top, a heading is scrolled to once it is drawn.
import { useCallback, useEffect, useState } from 'react';
import { HelpError, loadGuide, loadGuidePage, type IGuidePage } from '@/next/features/help/api';
import HelpView from '@/next/features/help/help-view';
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

const HelpPage = ({
  machine,
  page,
  anchor,
  onOpenDrawer,
}: {
  machine: MachineId;
  page: string | null;
  anchor: string | null;
  onOpenDrawer: () => void;
}) => {
  const lang = locale();
  const guide = useAsk<IGuidePage[]>(useCallback(() => loadGuide(machine, lang), [machine, lang]));
  const pages = guide.data;
  // The address names a page, else the first one is shown; a name the guide does not have is the first too.
  const current = pages ? ((pages.find((p) => p.name === page) ?? pages[0])?.name ?? null) : null;
  const loadOne = useCallback(() => loadGuidePage(machine, lang, current ?? ''), [machine, lang, current]);
  const one = useAsk<string>(current ? loadOne : null);
  const markdown = current ? one.data : null;

  // A heading named by the address, once the page is drawn; a page without one starts at its top.
  useEffect(() => {
    if (markdown === null) return;
    const target = anchor ? document.getElementById(anchor) : null;
    if (target) target.scrollIntoView({ block: 'start' });
    else document.querySelector('.hx-help')?.scrollTo({ top: 0 });
  }, [markdown, current, anchor]);

  return (
    <HelpView
      pages={pages}
      pagesError={guide.error}
      current={current}
      markdown={markdown}
      pageError={current ? one.error : null}
      onOpen={(name, to) => goTo({ kind: ROUTE.help, page: name, ...(to ? { anchor: to } : {}) })}
      onRetry={guide.error ? guide.retry : one.retry}
      onOpenDrawer={onOpenDrawer}
    />
  );
};

export default HelpPage;
