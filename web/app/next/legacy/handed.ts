// Which tabs of the old (Legacy) pane the new pane shows itself and so takes over: its own browser, plan and side chat,
// and a document or a board that an app shows (one document never stands in two views of the pane).
import type { TPaneTabKind } from '@/lib/hl-pane-tabs';

/** Legacy's tabs the new pane has tools of its own for: what Legacy's pane gets for them is handed over. */
const HANDED: ReadonlySet<TPaneTabKind> = new Set(['browser', 'plan', 'side-chat']);

/** Whether an address of a document or a board belongs to an app's tab of the new pane; null while that is not known. */
export type TLegacyClaims = ((target: string) => boolean) | null;

/** The tabs of the old pane that the new one shows itself: the browser, the plan, the side chat, and a document or a
 *  board an app shows, so that one is never open in two views. */
export const handedTabs = <T extends { kind: TPaneTabKind; target?: string | null }>(
  tabs: readonly T[],
  claims: TLegacyClaims,
): T[] =>
  tabs.filter((x) => HANDED.has(x.kind) || (x.kind === 'artifacts' && !!x.target && !!claims?.(x.target)));
