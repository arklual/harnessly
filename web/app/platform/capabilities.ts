import { appFetch } from '@/lib/app-path';

/** GET /api/capabilities (hub and runtime answer the same shape; HRN-737). */
export type TCapabilityStatus = 'on' | 'off' | 'needs_config';

export interface ICapability {
  id: string;
  status: TCapabilityStatus;
  /** Why it is not on. */
  reason?: string;
  /** The setting that turns it on. */
  configure?: string;
}

export interface ICapabilities {
  server: 'hub' | 'runtime';
  capabilities: ICapability[];
}

/** Reads the document; null when the server does not answer it (an older server): nothing is known, not "off". */
export const fetchCapabilities = async (base = ''): Promise<ICapabilities | null> => {
  try {
    const res = await appFetch(`${base}/api/capabilities`, { headers: { accept: 'application/json' } });
    if (!res.ok) return null;
    const doc = (await res.json()) as Partial<ICapabilities>;
    return Array.isArray(doc.capabilities) ? (doc as ICapabilities) : null;
  } catch {
    return null;
  }
};

/** The capability by id, or undefined for one the server does not know. */
export const findCapability = (doc: ICapabilities | null, id: string): ICapability | undefined =>
  doc?.capabilities.find((c) => c.id === id);

/** True only when the server says it is on; an unknown capability or a missing document is not on. */
export const isCapabilityOn = (doc: ICapabilities | null, id: string): boolean =>
  findCapability(doc, id)?.status === 'on';
