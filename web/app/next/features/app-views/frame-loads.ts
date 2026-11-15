// A bridge belongs to the document the frame first loaded (the one the view server wrote the nonce into). Every load
// after that one is the frame having navigated: a link, a form, a script's location. The new document is not the view
// the host mounted, so the host drops what it was talking to and the frame stays unheard until the host mounts it anew.

/**
 * Whether the frame shows the empty document and not a view: a frame whose address was taken away, or one given an
 * address that differs only in its fragment right after, stays on about:blank and still fires `load`. Only a
 * frame whose document the page may read can be on it; a view's own document is never readable from the page.
 */
export const isBlankDocument = (frame: HTMLIFrameElement): boolean => {
  try {
    return frame.contentWindow?.location.href === 'about:blank';
  } catch {
    return false;
  }
};

/**
 * Calls `onNavigated` on every `load` of the frame after the first. A load of the empty document (`isBlank`) is no
 * document of the view and is not counted. Returns the stop.
 */
export const watchLoads = (
  frame: EventTarget,
  onNavigated: () => void,
  isBlank: () => boolean = () => false,
): (() => void) => {
  let loads = 0;
  const onLoad = () => {
    if (isBlank()) return;
    loads += 1;
    if (loads > 1) onNavigated();
  };
  frame.addEventListener('load', onLoad);
  return () => frame.removeEventListener('load', onLoad);
};
