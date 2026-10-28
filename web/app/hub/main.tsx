import '@kit';
import './hub.css';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { startTheme } from '@kit';

import { App } from './app';
import { browserEnv, EnvProvider, type HubEnv } from './lib/env';
import { initLang } from './lib/i18n';

startTheme({ storageKey: 'hl-theme', fallback: 'system' });
initLang();

const mount = (env: HubEnv) => {
  const root = document.getElementById('root');
  if (!root) return;
  createRoot(root).render(
    <StrictMode>
      <EnvProvider env={env}>
        <App />
      </EnvProvider>
    </StrictMode>,
  );
};

// `?dev=<name>` runs the real screens against a fake hub in the page (dev/index.ts), so every state can be looked at
// without a hub. The fake is its own chunk, fetched only then.
const dev = new URLSearchParams(location.search).get('dev');
if (dev !== null) {
  void import('./dev').then((m) => {
    const root = document.getElementById('root');
    if (dev === '' && root) m.renderIndex(root);
    else mount(m.devEnv(dev));
  });
} else {
  mount(browserEnv());
}
