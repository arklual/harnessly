/** Apple keyboards: ⌘ instead of Ctrl in labels and chords. */
export const isMac: boolean = (() => {
  if (typeof navigator === 'undefined') return false;
  const platform =
    (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ||
    navigator.platform ||
    '';
  return /mac|iphone|ipad|ipod/i.test(platform);
})();
