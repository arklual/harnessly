import './boot';
import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';
import '@/next/styles/guest.css';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '@/platform/theme';
import GuestApp from '@/next/features/designly/guest/guest-app';
import { readGuestConfig } from '@/next/features/designly/guest/guest-config';

const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <ThemeProvider>
        <GuestApp config={readGuestConfig()} />
      </ThemeProvider>
    </StrictMode>,
  );
}
