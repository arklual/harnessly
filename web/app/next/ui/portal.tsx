import { createContext, useContext, type RefObject } from 'react';

/**
 * Where the new interface's popups (menus, popovers) mount: an element inside `.hx-root`, so they take its tokens, type
 * and box model instead of landing in the page's body under the current interface's styles.
 */
export const PortalContext = createContext<RefObject<HTMLDivElement | null> | null>(null);

export const usePortalContainer = () => useContext(PortalContext);
