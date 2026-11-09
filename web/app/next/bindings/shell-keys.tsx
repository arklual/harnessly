// The shell's shortcuts (features/keys): the search, the menu, a new chat, Settings, the next newer or older chat and
// the shortcuts sheet. The sheet lists the screens open when it was asked for.
import { useState } from 'react';
import { KEY_SCOPE, type TKeyScope } from '@/next/features/keys/commands';
import KeysHelp from '@/next/features/keys/keys-help';
import { keyScopesStore, useKeyOverrides, useKeys } from '@/next/features/keys/use-keys';
import { useBeta } from '@/next/stores/beta';
import { HOME, ROUTE, type TRoute } from '@/next/routes/routes';

interface IShellKeysChat {
  id: string;
  lastActivityAt: number;
}

interface IShellKeys {
  chats: readonly IShellKeysChat[] | null;
  currentId: string | null;
  navigate: (route: TRoute) => void;
  openChat: (id: string) => void;
  toggleSearch: () => void;
  toggleRail: () => void;
  /** ⌘⌥J: the navigator of the open chat (beta «chatNav»). */
  toggleNavigator?: () => void;
}

/** The chat next to the open one by its last activity: `dir` -1 is newer, 1 older; the newest when none is open. */
export const chatBeside = (
  chats: readonly IShellKeysChat[],
  currentId: string | null,
  dir: 1 | -1,
): string | null => {
  const order = [...chats].sort((a, b) => b.lastActivityAt - a.lastActivityAt);
  const at = order.findIndex((c) => c.id === currentId);
  if (at < 0) return order[0]?.id ?? null;
  return order[at + dir]?.id ?? null;
};

/** The n-th (1-based) chat of the list by last activity, newest first; null when the list is shorter. */
export const chatAt = (chats: readonly IShellKeysChat[], n: number): string | null =>
  [...chats].sort((a, b) => b.lastActivityAt - a.lastActivityAt)[n - 1]?.id ?? null;

export const useShellKeys = ({
  chats,
  currentId,
  navigate,
  openChat,
  toggleSearch,
  toggleRail,
  toggleNavigator,
}: IShellKeys) => {
  const { chatNav } = useBeta();
  const overrides = useKeyOverrides();
  const [help, setHelp] = useState<{ open: boolean; scopes: readonly TKeyScope[] }>({
    open: false,
    scopes: [],
  });
  const step = (dir: 1 | -1) => {
    const id = chats && chatBeside(chats, currentId, dir);
    if (id) openChat(id);
  };
  useKeys(KEY_SCOPE.global, {
    'global.help': () =>
      setHelp((h) => (h.open ? { ...h, open: false } : { open: true, scopes: keyScopesStore.get() })),
    'global.search': toggleSearch,
    'global.rail': toggleRail,
    'global.newChat': () => navigate(HOME),
    'global.settings': () => navigate({ kind: ROUTE.settings, section: null }),
    'global.newerChat': () => step(-1),
    'global.olderChat': () => step(1),
    ...(chatNav
      ? {
          ...(toggleNavigator ? { 'global.navigator': () => toggleNavigator() } : {}),
          ...Object.fromEntries(
            Array.from({ length: 9 }, (_, i) => [
              `global.chat${i + 1}`,
              () => {
                const id = chats && chatAt(chats, i + 1);
                if (!id) return false;
                openChat(id);
              },
            ]),
          ),
        }
      : {}),
  });
  const close = () => setHelp((h) => ({ ...h, open: false }));
  return (
    <KeysHelp
      open={help.open}
      onClose={close}
      scopes={help.scopes}
      overrides={overrides}
      onSettings={() => {
        close();
        navigate({ kind: ROUTE.settings, section: 'keys' });
      }}
    />
  );
};
