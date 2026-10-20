// Page contract of the app: a page component may say how it is laid out and whether it needs the runtime's start data.
import type { ComponentType, ReactElement, ReactNode } from 'react';

export type TNextPageWithLayout<P = object> = ComponentType<P> & {
  getLayout?: (page: ReactElement) => ReactNode;
  /** The page takes its data from the runtime's `/bootstrap`. */
  bootstrap?: boolean | { skipPreflight?: boolean };
};
