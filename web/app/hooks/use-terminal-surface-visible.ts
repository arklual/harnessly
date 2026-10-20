import { createContext, useContext } from 'react';

/** Whether the terminals under this subtree are on screen: false inside a hidden workspace pane or a hidden machine frame. */
export const TerminalSurfaceVisibleContext = createContext(true);

const useTerminalSurfaceVisible = () => useContext(TerminalSurfaceVisibleContext);

export default useTerminalSurfaceVisible;
