// A board or a document the agent opens goes where the person already works with it: when the page shows
// the boards' or the documents' own app for this chat, the app opens it, and the right pane is left as it is. The app's
// page claims the chat's opens while it is on screen; the pane asks it before it places a tab.

/** Takes an address the agent asked to open; true when the page opened it itself. */
export type TSpaceClaim = (href: string) => boolean;

const claims = new Map<string, TSpaceClaim>();

/** The page of a space claims the chat's opens while it is shown; the returned function lets go. */
export const claimSpaceOpens = (chat: string, claim: TSpaceClaim): (() => void) => {
  claims.set(chat, claim);
  return () => {
    if (claims.get(chat) === claim) claims.delete(chat);
  };
};

/** Whether the page of a space opened the address itself; the pane then leaves it. */
export const claimedBySpace = (chat: string, href: string | undefined): boolean =>
  href !== undefined && (claims.get(chat)?.(href) ?? false);
