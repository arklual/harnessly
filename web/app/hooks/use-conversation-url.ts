import { useCallback, useEffect, useEffectEvent, useRef, useState } from 'react';
import Router, { useRouter } from 'next/router';
import { APP_BASE_PATH } from '@/lib/app-path';
import { afterBackLayers, isBackLayerPop } from '@/lib/hl-back-layers';
import {
  locationLooselyMatches,
  locationMatches,
  parseAppLocation,
  searchForWanted,
  type IAppLocation,
  type IWantedLocation,
} from '@/lib/hl-conversation-url';

interface IUseConversationUrlOptions {
  /** What the address should say for the state on screen; null leaves the address alone. */
  wanted: IWantedLocation | null;
  /** The address moved (Back, Forward, a link, the first load) to a place the screen does not show yet. */
  onLocation: (location: IAppLocation) => void;
}

const queryOf = (search: string): Record<string, string> => Object.fromEntries(new URLSearchParams(search));

/** The app lives at `/agents/` (inside the service worker's scope); Next drops the slash on every navigation, so it is put back. */
const restoreAppSlash = () => {
  if (window.location.pathname !== APP_BASE_PATH) return;
  window.history.replaceState(
    window.history.state,
    '',
    `${APP_BASE_PATH}/${window.location.search}${window.location.hash}`,
  );
};

/**
 * The address follows the conversation on screen (a new history entry per choice, so Back and Forward work),
 * and the screen follows the address when it is moved from outside.
 */
const useConversationUrl = ({ wanted, onLocation }: IUseConversationUrlOptions) => {
  const router = useRouter();
  const [armed, setArmed] = useState(false);
  const [settled, setSettled] = useState(0);
  const previous = useRef<IWantedLocation | null>(null);
  const navigating = useRef(false);
  const replaceOnce = useRef(false);
  const moveScreen = useEffectEvent(onLocation);

  const followAddress = useEffectEvent(() => {
    if (navigating.current || !wanted) return;
    const location = parseAppLocation(window.location.search);
    if (location.shared || locationMatches(location, wanted)) return;
    moveScreen(location);
  });

  const followScreen = useEffectEvent((replace: boolean) => {
    if (!wanted || navigating.current) return;
    const { search } = window.location;
    const location = parseAppLocation(search);
    if (location.shared) return;
    if (locationMatches(location, wanted) && !location.legacy) return;
    const next = searchForWanted(search, wanted);
    const mode = replace || location.legacy || locationLooselyMatches(location, wanted) ? 'replace' : 'push';
    navigating.current = true;
    router[mode]({ pathname: '/workspace', query: queryOf(next) }, `/${next}`, { shallow: true })
      .then(() => {
        restoreAppSlash();
        navigating.current = false;
        setSettled((count) => count + 1);
      })
      .catch(() => {
        navigating.current = false;
      });
  });

  useEffect(() => {
    if (!router.isReady) return;
    restoreAppSlash();
    followAddress();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the address is read from the browser only after hydration; the screen follows it from the next render on
    setArmed(true);
  }, [router.isReady]);

  useEffect(() => {
    if (!armed) return;
    // A step back over a closed drawer's entry is not a move of the address: following it took the screen
    // back to where it was before the tap that closed the drawer, so a conversation chosen on a phone never opened.
    const onPopState = (event: PopStateEvent) =>
      window.setTimeout(() => {
        if (!isBackLayerPop(event)) followAddress();
      }, 0);
    const onRouteChange = () => {
      restoreAppSlash();
      followAddress();
    };
    window.addEventListener('popstate', onPopState);
    Router.events.on('routeChangeComplete', onRouteChange);
    return () => {
      window.removeEventListener('popstate', onPopState);
      Router.events.off('routeChangeComplete', onRouteChange);
    };
  }, [armed]);

  const wantedKey = wanted?.key ?? null;
  const wantedView = wanted?.view ?? null;
  const wantedProvisional = wanted?.provisional ?? null;
  const wantedProject = wanted?.design?.project ?? null;
  const wantedBoard = wanted?.design?.board ?? null;
  const wantedFull = wanted?.design?.full ?? false;
  const hasWanted = wanted !== null;
  useEffect(() => {
    if (!armed || !hasWanted) return;
    const replace = (previous.current?.provisional ?? false) || replaceOnce.current;
    replaceOnce.current = false;
    previous.current = { key: wantedKey, view: wantedView, provisional: wantedProvisional ?? false };
    afterBackLayers(() => followScreen(replace));
  }, [
    armed,
    settled,
    hasWanted,
    wantedKey,
    wantedView,
    wantedProvisional,
    wantedProject,
    wantedBoard,
    wantedFull,
  ]);

  /** The next choice rewrites the current entry instead of adding one: moving through the sidebar's own history is not a new step. */
  const replaceNext = useCallback(() => {
    replaceOnce.current = true;
  }, []);
  return { replaceNext };
};

export default useConversationUrl;
