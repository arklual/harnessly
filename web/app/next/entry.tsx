// The new interface's lazy root: it boots v2 (boot.ts: cache → me → machines → sockets) and draws the shell on
// its stores. main.tsx loads this chunk only for a person who chose the new interface; when the person goes (another
// account, sign-out) the boot lets everything of theirs go.

import { useEffect } from 'react';
import Shell, { type IShellProps } from '@/next/bindings/shell';
import { bootNext, shutdownNext } from '@/next/boot';

const NextEntry = (props: IShellProps) => {
  useEffect(() => {
    void bootNext();
    return () => void shutdownNext();
  }, [props.owner]);
  return <Shell {...props} />;
};

export default NextEntry;
