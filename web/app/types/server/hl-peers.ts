// Shapes of what the runtime answers; the runtime itself is Go.

export interface IPeerSession {
  name: string;
  pid: number;
  sessionId: string | null;
  tabId: string | null;
  status: string | null;
  updatedAt: number | null;
}
