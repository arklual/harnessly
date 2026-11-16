// Puts the look of the place the person is in on the page: the open chat's own look, else its folder's, else the one
// for all (features/looks/types.ts lookFor); none leaves the app exactly as it always was. The tokens go on <html> as
// inline custom properties, the variants of the chat's parts as html[data-look-*] (styles/looks.css), a forced light or
// dark through the theme (platform/theme.tsx setForcedMode). A change is written only when it differs, in one pass
// with the transitions off, so moving between chats of one look costs nothing and between two looks one style pass.
import { useLayoutEffect, useMemo, useSyncExternalStore } from 'react';
import { lookFor, type ILook } from '@/next/features/looks/types';
import { lookTokens, type TMode } from '@/next/features/looks/tokens';
import { useLooks } from '@/next/features/looks/store';
import { setForcedMode, useTheme } from '@/platform/theme';

/** The look on the page now and the light or dark it is drawn in: the backdrop reads it (backdrop.tsx). */
export interface IActiveLook {
  look: ILook;
  mode: TMode;
  /** The open chat, null outside one (a live effect hears its agent: fx-layer.tsx). */
  chat: string | null;
}

let active: IActiveLook | null = null;
const listeners = new Set<() => void>();
const publish = (next: IActiveLook | null) => {
  if (active === next) return;
  active = next;
  listeners.forEach((fn) => fn());
};
const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => listeners.delete(fn);
};
export const useActiveLook = (): IActiveLook | null =>
  useSyncExternalStore(
    subscribe,
    () => active,
    () => null,
  );

const ATTRS = ['bubble', 'turns', 'composer', 'sidebar', 'density', 'font', 'live'] as const;
type TAttr = (typeof ATTRS)[number];
const attrName = (a: TAttr) => `look${a[0].toUpperCase()}${a.slice(1)}`;

/** What is on <html> now, to write only what changed. */
let painted: { tokens: Record<string, string>; attrs: Partial<Record<TAttr, string>> } = {
  tokens: {},
  attrs: {},
};

const attrsOf = (look: ILook | null): Partial<Record<TAttr, string>> => {
  if (!look) return {};
  const out: Partial<Record<TAttr, string>> = {};
  if (look.parts.bubble) out.bubble = look.parts.bubble;
  if (look.parts.turns) out.turns = look.parts.turns;
  if (look.parts.composer) out.composer = look.parts.composer;
  if (look.parts.sidebar) out.sidebar = look.parts.sidebar;
  if (look.density) out.density = look.density;
  if (look.font) out.font = look.font;
  // A scene moves under the chat: the glass parts drop their backdrop blur (styles/looks.css).
  if (look.bg.kind === 'scene') out.live = 'scene';
  return out;
};

const same = (a: Record<string, string>, b: Record<string, string>) => {
  const ka = Object.keys(a);
  return ka.length === Object.keys(b).length && ka.every((k) => a[k] === b[k]);
};

/** Writes the look's tokens and variants on <html>; true when anything changed. */
const paintLook = (tokens: Record<string, string>, attrs: Partial<Record<TAttr, string>>): boolean => {
  if (same(painted.tokens, tokens) && same(painted.attrs, attrs)) return false;
  const root = document.documentElement;
  root.dataset.lookSwitch = '';
  for (const name of Object.keys(painted.tokens))
    if (!(name in tokens)) root.style.removeProperty(`--${name}`);
  for (const [name, value] of Object.entries(tokens))
    if (painted.tokens[name] !== value) root.style.setProperty(`--${name}`, value);
  for (const a of ATTRS) {
    const value = attrs[a];
    if (value === undefined) delete root.dataset[attrName(a)];
    else root.dataset[attrName(a)] = value;
  }
  painted = { tokens, attrs };
  // The transitions come back after the new look was drawn once.
  requestAnimationFrame(() => requestAnimationFrame(() => delete root.dataset.lookSwitch));
  return true;
};

export interface ILookLayerProps {
  /** The open chat, null outside a chat. */
  chatId: string | null;
  /** The sidebar folder of the open chat, null for none. */
  folderId: string | null;
}

/** Mounted once in the shell; draws nothing itself. */
const LookLayer = ({ chatId, folderId }: ILookLayerProps) => {
  const looks = useLooks();
  const { naturalTheme } = useTheme();
  const look = lookFor(looks, chatId, folderId);
  const mode: TMode = look && look.mode !== 'auto' ? look.mode : naturalTheme;
  const tokens = useMemo(() => (look ? lookTokens(look, mode) : {}), [look, mode]);
  useLayoutEffect(() => {
    setForcedMode(look && look.mode !== 'auto' ? look.mode : null);
    paintLook(tokens, attrsOf(look));
    publish(look ? { look, mode, chat: chatId } : null);
  }, [look, mode, tokens, chatId]);
  return null;
};

export default LookLayer;
