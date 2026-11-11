// The new interface's lazy root: it boots v2 (boot.ts: cache → me → machines → sockets) and draws the shell on
// its stores. main.tsx loads this chunk only for a person who chose the new interface; when the person goes (another
// account, sign-out) the boot lets everything of theirs go.

import '@/next/styles/order';

import { useEffect } from 'react';
import Shell, { type IShellProps } from '@/next/bindings/shell';
import { bootNext, shutdownNext } from '@/next/boot';

const NextEntry = (props: IShellProps) => {
  // Booted once per mount: the owner may arrive after the first draw (a home machine slow to give the start data), and
  // the boot finds who is signed in by itself (me), so a late owner is no other person.
  useEffect(() => {
    void bootNext();
    return () => void shutdownNext();
  }, []);
  return <Shell {...props} />;
};

export default NextEntry;
