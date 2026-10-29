// A link revoked or expired while its page is open: the gateway answers 410 to every address of it. The page stops asking and
// loads once more, so the guest sees the gateway's own page that says what happened to the link. Loaded again within a minute
// (the gateway still served the app), it only stops asking: a loop of reloads would be worse than a stale page.
export const LINK_GONE = 410;

const RELOAD_GAP_MS = 60_000;

export const leaveGoneLink = (token: string): void => {
  const key = `guest-gone:${token}`;
  try {
    const last = Number(window.sessionStorage.getItem(key));
    if (Number.isFinite(last) && Date.now() - last < RELOAD_GAP_MS) return;
    window.sessionStorage.setItem(key, String(Date.now()));
  } catch {
    return;
  }
  window.location.reload();
};
