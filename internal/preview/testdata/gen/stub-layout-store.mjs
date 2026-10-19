export const parseSessionName = (name) => {
  const m = /^hs-(ws-.*?)-(pane-.*?)-(tab-.+)$/.exec(name);
  return m ? { wsId: m[1], paneId: m[2], tabId: m[3] } : null;
};
