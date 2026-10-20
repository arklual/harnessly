/** A frame of the tab's preview: true when its bridge was asked to show the element. */
type THighlightListener = (selector: string) => boolean;

/** A composer chip asks the open preview of the same tab to show its element. */
export const createHighlightChannel = () => {
  const byTab = new Map<string, Set<THighlightListener>>();
  return {
    request: (tab: string, selector: string): boolean => {
      let shown = false;
      for (const listener of byTab.get(tab) ?? []) shown = listener(selector) || shown;
      return shown;
    },
    listen: (tab: string, listener: THighlightListener): (() => void) => {
      let listeners = byTab.get(tab);
      if (!listeners) byTab.set(tab, (listeners = new Set()));
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
        if (!listeners.size && byTab.get(tab) === listeners) byTab.delete(tab);
      };
    },
  };
};

const channel = createHighlightChannel();

export const highlightInPreview = (tab: string, selector: string): boolean => channel.request(tab, selector);
export const onHighlightRequest = (tab: string, listener: THighlightListener): (() => void) =>
  channel.listen(tab, listener);
