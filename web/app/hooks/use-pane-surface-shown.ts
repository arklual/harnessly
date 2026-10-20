import { createContext, useContext } from 'react';

/**
 * Whether this part of the right panel is on screen: false inside a folded island or a tab behind another. Such parts
 * stay mounted to keep their counts current, but heavy views (a diff with highlighted code) draw nothing meanwhile.
 */
export const PaneSurfaceShownContext = createContext(true);

const usePaneSurfaceShown = () => useContext(PaneSurfaceShownContext);

export default usePaneSurfaceShown;
