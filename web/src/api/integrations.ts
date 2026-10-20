import { HttpError, request } from './http';

/** Where the sibling services are; null for one this runtime has no address for. */
export interface Integrations {
  boards: { url: string } | null;
  artifacts: { url: string } | null;
}

export interface ConversationDocument {
  slug: string;
  title: string;
  updatedAt: string;
  version: number;
  viewUrl: string;
  embedUrl: string;
}

export const none: Integrations = { boards: null, artifacts: null };

export const integrations = {
  /** The services' origins; a runtime without the routes (an older one, or the previous runtime) has none. */
  get: async (): Promise<Integrations> => {
    try {
      return (await request<Integrations>('/api/integrations')) ?? none;
    } catch (err) {
      if (err instanceof HttpError && err.status === 404) return none;
      throw err;
    }
  },
  /** The address to load in the board pane: a one-time sign-in that lands on the board, or the board itself. */
  boardSignIn: (next: string) =>
    request<{ url: string }>('/api/integrations/boards/sign-in', { body: { next } }),
  documents: (session: string) =>
    request<{ documents: ConversationDocument[] }>(
      `/api/integrations/artifacts/documents?session=${encodeURIComponent(session)}`,
    ),
};
