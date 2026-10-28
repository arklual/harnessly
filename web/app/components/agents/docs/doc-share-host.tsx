// The share dialog outside the new interface's frame: the page `/docs/<slug>` of a standalone runtime has no `.hx-root`
// for the dialog to take its tokens and type from, and no place for its popups. This host gives it both. It is loaded
// only there (doc-share.tsx), so its stylesheets never reach the frame, where they would repeat the shell's own.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';

import { useRef, type ReactNode } from 'react';
import { PortalContext } from '@/next/ui/portal';

const DocShareHost = ({ children }: { children: ReactNode }) => {
  const portal = useRef<HTMLDivElement | null>(null);
  return (
    <PortalContext value={portal}>
      <div className="hx-root td-share-host">
        {children}
        <div ref={portal} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default DocShareHost;
