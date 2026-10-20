import Router from 'next/router';

interface ILayer {
  close: () => void;
  pushed: boolean;
}

const stack: ILayer[] = [];
/** Entries the page itself steps back over after a layer was closed by a tap, not by Back. */
let silentPops = 0;
let handledPop = false;
/** Steps back the layers took for themselves: nobody else's navigation, whichever listener heard the event first. */
const ownPops = new WeakSet<Event>();
let installed = false;
const idleWaiters: (() => void)[] = [];
const IDLE_FALLBACK_MS = 600;

const flushIdleWaiters = () => {
  if (silentPops > 0) return;
  idleWaiters.splice(0).forEach((run) => run());
};

const install = () => {
  if (installed) return;
  installed = true;
  window.addEventListener('popstate', (event) => {
    if (silentPops === 0 && stack.length === 0) return;
    ownPops.add(event);
    handledPop = true;
    setTimeout(() => {
      handledPop = false;
    }, 0);
    if (silentPops > 0) {
      silentPops--;
      setTimeout(flushIdleWaiters, 0);
      return;
    }
    stack.pop()?.close();
  });
  // Stepping off a layer lands on the page's own entry; for Next that is not a navigation.
  Router.beforePopState(() => !(handledPop || silentPops > 0 || stack.length > 0));
};

/**
 * Gives an open panel its own history entry, so Back (Safari's button or edge swipe) closes the panel
 * instead of leaving the app. The returned release is for a panel closed by the page itself.
 */
export const pushBackLayer = (close: () => void): (() => void) => {
  install();
  const layer: ILayer = { close, pushed: false };
  stack.push(layer);
  // Deferred a tick: a layer opened and released at once (StrictMode, a quick toggle) never touches history.
  const timer = window.setTimeout(() => {
    layer.pushed = true;
    window.history.pushState({ twLayer: stack.length }, '');
  }, 0);
  return () => {
    window.clearTimeout(timer);
    const index = stack.indexOf(layer);
    if (index === -1) return;
    stack.splice(index, 1);
    if (!layer.pushed) return;
    silentPops++;
    window.history.back();
  };
};

/**
 * True for a step back that closed a layer or took a closed layer's entry off: the page's own address did not move.
 * Ask after the event's listeners have all run (a task later): the layers' listener is added when the first layer
 * opens, which on a phone is after the address listener.
 */
export const isBackLayerPop = (event: Event): boolean => ownPops.has(event);

/**
 * Runs `run` once the page has stepped back over every entry a closed layer left behind, so a navigation pushed
 * meanwhile is not the entry that Back then takes off.
 */
export const afterBackLayers = (run: () => void) => {
  if (silentPops === 0) {
    run();
    return;
  }
  let done = false;
  const once = () => {
    if (done) return;
    done = true;
    run();
  };
  idleWaiters.push(once);
  setTimeout(once, IDLE_FALLBACK_MS);
};
