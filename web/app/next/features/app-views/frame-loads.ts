// A bridge belongs to the document the frame first loaded (the one the view server wrote the nonce into). Every load
// after that one is the frame having navigated: a link, a form, a script's location. The new document is not the view
// the host mounted, so the host drops what it was talking to and the frame stays unheard until the host mounts it anew.

/** Calls `onNavigated` on every `load` of the frame after the first. Returns the stop. */
export const watchLoads = (frame: EventTarget, onNavigated: () => void): (() => void) => {
  let loads = 0;
  const onLoad = () => {
    loads += 1;
    if (loads > 1) onNavigated();
  };
  frame.addEventListener('load', onLoad);
  return () => frame.removeEventListener('load', onLoad);
};
