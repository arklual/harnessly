import type { ITab } from '@/types/terminal';

/** Props every pluggable island view receives from the right panel (conversation-pane). */
export interface IInspectorIslandProps {
  tab: ITab | null;
  provider: string;
  /** True in a tab of the right panel, where the view has the whole height. */
  expanded: boolean;
}
