// The screens of the last tabs that show a frame (a design in Sinopia, an app's space: the boards, the documents, a
// document, a review, a kit) stay mounted while another tab is in front: switching back finds the page as it was, with
// no «Загружаем…». A frame cannot be moved in the document without loading again, so each kept screen stands in one
// place under the shell for its whole life and is only hidden (KeptScreens in bindings/shell.tsx). More than
// MAX_KEPT_SCREENS go, the least recent first: what they keep of a tab (its page, its scroll, its view state) is the
// tab's, so they open where they were.

/** Screens kept mounted, the one on screen included. */
export const MAX_KEPT_SCREENS = 5;

/**
 * How many screens a device keeps (measured with CPU x4 on a real project: a kept design costs about 5.5 MB
 * of JS heap and 10–15 MB of memory, no process of its own, and no CPU while hidden): five, three on a device with
 * 4 GB or less, two with 2 GB or less. Without `navigator.deviceMemory` (Safari, Firefox) five.
 */
export const keptScreensMax = (deviceMemory: number | undefined): number => {
  if (deviceMemory === undefined || !Number.isFinite(deviceMemory)) return MAX_KEPT_SCREENS;
  if (deviceMemory <= 2) return 2;
  if (deviceMemory <= 4) return 3;
  return MAX_KEPT_SCREENS;
};

/**
 * The tabs whose screens stay mounted, the most recent first (what goes first when there are too many; they are drawn in
 * a fixed order): the active one (when it shows a kept kind), then the ones
 * kept before that still exist and still show a kept kind, up to `max`.
 */
export const keptScreens = (
  before: readonly string[],
  active: string,
  tabs: readonly { id: string; path: string }[],
  keeps: (path: string) => boolean,
  max: number = MAX_KEPT_SCREENS,
): readonly string[] => {
  const kind = new Map(tabs.map((tab) => [tab.id, keeps(tab.path)]));
  const next = [active, ...before.filter((id) => id !== active)]
    .filter((id) => kind.get(id) === true)
    .slice(0, max);
  return next.length === before.length && next.every((id, i) => id === before[i]) ? before : next;
};

/** The element stands in a kept screen that waits hidden: the page-wide keys it listens to are not its own now. */
export const inHiddenScreen = (el: Element | null | undefined): boolean =>
  el?.closest('.hx-kept-screen[data-on="false"]') != null;
