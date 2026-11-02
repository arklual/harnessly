// Designly as a tab of the right pane. The chat on the left is the field that starts a design, so the tab holds only
// the library: search, «Все / Мои / Общие» and the cards. A card opens the design in the pane as the whole viewer (the
// design's page, its bar, the devices, the pages, the page's settings); the chat beside it edits the design — a block
// picked on the page lands in that chat's field, and what the agent writes into the board shows on the page.
// A card of a document of an app's type opens the app's editor in the pane instead (read-only when it may only be read).
import { useEffect, useState } from 'react';
import { DesignlyLibrary } from '@/next/bindings/designly';
import { DesignlyPaneSite } from '@/next/bindings/designly-site';
import { useAppDocStage } from '@/next/bindings/app-doc';
import { loadKindDocs } from '@/next/features/app-catalog/api';
import { Button } from '@/next/ui/button';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

export interface IDesignlyPaneProps {
  machine: MachineId;
  /** The chat the pane is beside. */
  conv: ConvId;
  /** The design on screen; null: the library. */
  design: string | null;
  onOpen: (id: string | null) => void;
}

/** The editor of an app's document in the pane, with the way back to the library. */
const PaneDoc = ({
  machine,
  id,
  conv,
  onBack,
}: {
  machine: MachineId;
  id: string;
  conv: ConvId;
  onBack: () => void;
}) => {
  const { stage } = useAppDocStage(machine, id, conv);
  return (
    <>
      <Button variant="ghost" onClick={onBack}>
        {t('next.designly.library')}
      </Button>
      {stage}
    </>
  );
};

/** A design or a document of an app's type: told by the list of such documents, which an older runtime has none of. */
const PaneDesign = ({
  machine,
  id,
  conv,
  onBack,
}: {
  machine: MachineId;
  id: string;
  conv: ConvId;
  onBack: () => void;
}) => {
  const [isDoc, setIsDoc] = useState<boolean | null>(null);
  useEffect(() => {
    let live = true;
    loadKindDocs(machine).then(
      (docs) => live && setIsDoc(docs.some((d) => d.document === id)),
      () => live && setIsDoc(false),
    );
    return () => {
      live = false;
    };
  }, [machine, id]);
  if (isDoc === null) return <div className="hx-appview__wait" aria-busy="true" />;
  return isDoc ? (
    <PaneDoc machine={machine} id={id} conv={conv} onBack={onBack} />
  ) : (
    <DesignlyPaneSite machine={machine} id={id} conv={conv} onBack={onBack} />
  );
};

const DesignlyPane = ({ machine, conv, design, onOpen }: IDesignlyPaneProps) =>
  design === null ? (
    <DesignlyLibrary machine={machine} onOpen={onOpen} />
  ) : (
    <PaneDesign key={design} machine={machine} id={design} conv={conv} onBack={() => onOpen(null)} />
  );

export default DesignlyPane;
