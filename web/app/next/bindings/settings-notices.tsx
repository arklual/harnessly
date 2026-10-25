// Settings → Notifications on the browser's own switches (stores/notices.ts) and its permission for notifications of
// the system, asked when the person turns that switch on.
import { useState } from 'react';
import NoticesSection from '@/next/features/settings/notices-section';
import { SYSTEM_PERMISSION, askSystemPermission, systemPermission } from '@/next/features/notifications/ping';
import { setNotify, useNotify } from '@/next/stores/notices';

export const NoticesPage = () => {
  const settings = useNotify();
  const [permission, setPermission] = useState(systemPermission);
  return (
    <NoticesSection
      settings={settings}
      permission={permission}
      onKind={(kind, on) => setNotify({ kinds: { [kind]: on } })}
      onSound={(sound) => setNotify({ sound })}
      onSystem={(on) => {
        if (!on) {
          setNotify({ system: false });
          return;
        }
        void askSystemPermission().then((answer) => {
          setPermission(answer);
          setNotify({ system: answer === SYSTEM_PERMISSION.granted });
        });
      }}
    />
  );
};
