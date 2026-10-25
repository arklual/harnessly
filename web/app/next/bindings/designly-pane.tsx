// Designly as a tab of the right pane. The chat on the left is the field that starts a design, so the tab holds only
// the library: search, «Все / Мои / Общие» and the cards. A card opens the design in the pane as the whole viewer (the
// design's page, its bar, the devices, the pages, the page's settings); the chat beside it edits the design — a block
// picked on the page lands in that chat's field, and what the agent writes into the board shows on the page.
import { DesignlyLibrary } from '@/next/bindings/designly';
import { DesignlyPaneSite } from '@/next/bindings/designly-site';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export interface IDesignlyPaneProps {
  machine: MachineId;
  /** The chat the pane is beside. */
  conv: ConvId;
  /** The design on screen; null: the library. */
  design: string | null;
  onOpen: (id: string | null) => void;
}

const DesignlyPane = ({ machine, conv, design, onOpen }: IDesignlyPaneProps) =>
  design === null ? (
    <DesignlyLibrary machine={machine} onOpen={onOpen} />
  ) : (
    <DesignlyPaneSite key={design} machine={machine} id={design} conv={conv} onBack={() => onOpen(null)} />
  );

export default DesignlyPane;
