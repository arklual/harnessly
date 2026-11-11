// The hub's host for extension views: which views the operator's extensions file declares (GET /api/ext/ui,
// there only while HARNESSLY_UI_SLOTS=on), the check of a message from a view's frame, and the hub's branding.
// A view runs in a sandboxed iframe on an opaque origin; a message is believed only from that frame's own window AND
// from the origin its messages carry.
import { useEffect, useState } from 'react';

export interface HostView {
  ext: string;
  id: string;
  label: string;
  icon?: string;
  url: string;
  origin: string;
}

export interface Branding {
  name?: string;
  accent?: string;
  supportUrl?: string;
}

export interface HostUI {
  views: HostView[];
  branding?: Branding;
}

const EMPTY: HostUI = { views: [] };
const ACCENT = /^#[0-9a-fA-F]{6}$/;

/** The accent is applied only as #rrggbb: the answer is data from an operator's file, not CSS. */
export const accentOf = (b: Branding | undefined): string | null =>
  b?.accent && ACCENT.test(b.accent) ? b.accent : null;

/** A view that is shown only from an https address: whatever else the answer holds is dropped. */
export const safeViews = (views: unknown): HostView[] => {
  if (!Array.isArray(views)) return [];
  return views.filter((v): v is HostView => {
    if (!v || typeof v !== 'object') return false;
    const x = v as Record<string, unknown>;
    if (typeof x.ext !== 'string' || typeof x.id !== 'string' || typeof x.label !== 'string') return false;
    if (typeof x.url !== 'string' || typeof x.origin !== 'string') return false;
    try {
      return new URL(x.url).protocol === 'https:';
    } catch {
      return false;
    }
  });
};

let cached: Promise<HostUI> | null = null;

/** The hub's extension UI. Off (404), unreachable or garbled: nothing, so the hub looks as it does without extensions. */
export const loadHostUI = (): Promise<HostUI> => {
  cached ??= fetch('/api/ext/ui', { credentials: 'same-origin', headers: { accept: 'application/json' } })
    .then(async (r) => {
      if (!r.ok) return EMPTY;
      const body = (await r.json()) as Partial<HostUI>;
      return { views: safeViews(body.views), branding: body.branding };
    })
    .catch(() => EMPTY);
  return cached;
};

/** For tests: forget the answer. */
export const forgetHostUI = (): void => {
  cached = null;
};

export const useHostUI = (): HostUI | null => {
  const [ui, setUI] = useState<HostUI | null>(null);
  useEffect(() => {
    let alive = true;
    void loadHostUI().then((u) => alive && setUI(u));
    return () => {
      alive = false;
    };
  }, []);
  return ui;
};

/** Whether a message is the frame's: its window and its origin (a sandboxed view's is "null"). */
export const fromFrame = (
  event: { source: unknown; origin: string },
  frame: Window | null | undefined,
  origin = 'null',
): boolean => !!frame && event.source === frame && event.origin === origin;

/** The hub's name and accent while the screen is open; put back when it closes. */
export const useBranding = (b: Branding | undefined): void => {
  useEffect(() => {
    const html = document.documentElement;
    const was = { title: document.title, accent: html.style.getPropertyValue('--hx-accent') };
    const accent = accentOf(b);
    if (b?.name) document.title = b.name;
    if (accent) html.style.setProperty('--hx-accent', accent);
    return () => {
      document.title = was.title;
      if (accent) {
        if (was.accent) html.style.setProperty('--hx-accent', was.accent);
        else html.style.removeProperty('--hx-accent');
      }
    };
  }, [b]);
};
