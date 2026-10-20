import { useDeferredValue, useSyncExternalStore } from 'react';
import type { IResourceState, ISharedResource } from '@/lib/shared-resource';

const EMPTY: IResourceState<never> = { data: null, error: null };
const noSubscription = () => () => undefined;
const emptySnapshot = () => EMPTY;

/**
 * The state of a shared resource for a component. A store update renders synchronously and cannot yield, so a poll
 * that brought a new list froze the page for the whole re-render of the sidebar or the composer (up to
 * 0.5 s attributed to `Response.json.then`). The new value is rendered as deferred work instead: the urgent pass
 * keeps the previous value and costs nothing, the deferred one yields to typing and clicks. A change made on this page
 * (`update`) and a different resource (another conversation, machine or folder) show at once.
 */
const useSharedResource = <T>(resource: ISharedResource<T> | null): IResourceState<T> => {
  const state = useSyncExternalStore(
    resource?.subscribe ?? noSubscription,
    resource?.getSnapshot ?? emptySnapshot,
    resource?.getServerSnapshot ?? emptySnapshot,
  );
  const deferredState = useDeferredValue(state);
  const deferredResource = useDeferredValue(resource);
  return state.local || deferredResource !== resource ? state : deferredState;
};

export default useSharedResource;
