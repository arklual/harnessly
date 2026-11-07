// The editor of a document with the apps on in it: the document's own view in the middle, the apps' panels on its right
// (only for the kinds they name), the menu of the selection, and the dialog that stops a share the checks forbid.
// Looking at the document (its mode «Макет»), only the share gate is left of them. Nothing here when no app is on in
// the document: the wrapper is layout-neutral (`display: contents`) so the editor is mounted once and never reloads
// when an app turns up.
import { useState, type ReactNode } from 'react';
import type { IAppView, IInstalledApp } from '@/next/features/app-views/api';
import AppFrame from '@/next/features/app-views/app-frame';
import { panelsOf, selectionActions, type IPanelRef } from '@/next/features/app-views/sinopia/contributions';
import type { ISinopia } from '@/next/features/app-views/sinopia/use-sinopia';
import type { ISinopiaCheckResult } from '@/next/features/app-views/sinopia/types';
import { Modal } from '@/next/features/designly/sdk/ui';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/sinopia.css';

/** The panel's exit, as long as its animation in sinopia.css. */
const EXIT_MS = 220;

/** What the panel shows: the inspector an element's click opened, or one of the apps' panels. */
interface IShown {
  app: IInstalledApp;
  view: IAppView;
}

const FindingLine = ({ result }: { result: ISinopiaCheckResult }) => (
  <li className="hx-sinopia-block__check">
    <span className="hx-sinopia-block__app">{result.title ?? result.appName}</span>
    <ul className="hx-sinopia-block__findings">
      {result.findings.slice(0, 8).map((f, i) => (
        <li key={`${f.node}-${i}`}>{f.message}</li>
      ))}
    </ul>
  </li>
);

export interface ISinopiaStageProps {
  machine: MachineId;
  /** The document's kind; null while the project is not read. */
  kind: string | null;
  /** The document's id. */
  id: string;
  conversation: ConvId | null;
  sinopia: ISinopia;
  /** The document is looked at: the apps' panels and the selection's menu go, the share gate stays. */
  viewing?: boolean;
  /** The document's own view. */
  children: ReactNode;
}

const SinopiaStage = ({
  machine,
  kind,
  id,
  conversation,
  sinopia,
  viewing = false,
  children,
}: ISinopiaStageProps) => {
  const { active, focus, selection, viewPort } = sinopia;
  const panels = panelsOf(active);
  const actions = selectionActions(active);
  const [hidden, setHidden] = useState(false);
  const [chosen, setChosen] = useState<string | null>(null);

  const panel: IPanelRef | null = panels.find((p) => p.key === chosen) ?? panels[0] ?? null;
  // A click on a bound element brings its app's inspector forward; without one the first panel is shown.
  const target: IShown | null = (() => {
    if (focus !== null) return { app: focus.app, view: focus.view };
    const view = panel?.app.views.find((v) => v.id === panel.view);
    return panel !== null && view !== undefined ? { app: panel.app, view } : null;
  })();
  const open =
    !viewing && target !== null && kind !== null && viewPort !== null && (focus !== null || !hidden);
  // The presence holds a value that stays the same object while it is the same view (a new one each render would swap
  // forever), and the last view while it leaves. State adjusted while rendering, as usePresence does.
  const [last, setLast] = useState<IShown | null>(null);
  if (target !== null && (last === null || last.app !== target.app || last.view !== target.view))
    setLast(target);
  const presence = usePresence(open ? last : null, EXIT_MS);
  const aside = presence.value;
  const bar = usePresence(!viewing && selection.length > 0 && actions.length > 0 ? 'bar' : null, EXIT_MS);

  return (
    <div className="hx-sinopia" data-aside={aside !== null || undefined}>
      <div className="hx-sinopia__main">{children}</div>
      {aside !== null && (
        <aside className="hx-sinopia__panel" data-state={presence.state} aria-label={t('next.sinopia.panel')}>
          <div className="hx-sinopia__head">
            {focus === null && panels.length > 1 ? (
              <div className="hx-sinopia__tabs" role="tablist">
                {panels.map((p) => (
                  <button
                    key={p.key}
                    type="button"
                    role="tab"
                    aria-selected={panel?.key === p.key}
                    className="hx-sinopia__tab"
                    onClick={() => setChosen(p.key)}
                  >
                    {p.title}
                  </button>
                ))}
              </div>
            ) : (
              <span className="hx-sinopia__title">
                {focus !== null ? (focus.view.title ?? focus.app.name) : (panel?.title ?? '')}
              </span>
            )}
            <button
              type="button"
              className="hx-head-btn"
              aria-label={t('next.sinopia.panelHide')}
              onClick={() => (focus !== null ? sinopia.closeFocus() : setHidden(true))}
            >
              <Icon name="x" />
            </button>
          </div>
          <div className="hx-sinopia__body">
            {viewPort !== null && kind !== null && (
              <AppFrame
                key={`${aside.app.id}/${aside.view.id}`}
                machine={machine}
                app={aside.app}
                view={aside.view}
                target={`sinopia.${kind}`}
                viewPort={viewPort}
                conversation={conversation}
                document={id}
                notice={sinopia.notice}
                onSinopiaCall={(method, params) => sinopia.onViewCall(aside.app, method, params)}
              />
            )}
          </div>
        </aside>
      )}
      {!viewing && target !== null && hidden && focus === null && (
        <button type="button" className="hx-sinopia__reopen" onClick={() => setHidden(false)}>
          {t('next.sinopia.panelShow')}
        </button>
      )}
      {bar.value !== null && (
        <div className="hx-sinopia__bar" data-state={bar.state}>
          {actions.map((a) => (
            <Button key={a.key} variant="ghost" onClick={() => void sinopia.runAction(a)}>
              {a.item.title}
            </Button>
          ))}
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.sinopia.selectionClear')}
            onClick={sinopia.clearSelection}
          >
            <Icon name="x" />
          </button>
        </div>
      )}
      <ShareBlocked results={sinopia.blocked} onClose={sinopia.dismissBlocked} />
    </div>
  );
};

/** The dialog that says why the document is not shared: what the checks found, the way the person reads it. */
export const ShareBlocked = ({
  results,
  onClose,
}: {
  results: ISinopiaCheckResult[] | null;
  onClose: () => void;
}) => {
  const presence = usePresence(results, EXIT_MS + 40);
  const shown = presence.value;
  if (shown === null) return null;
  return (
    <Modal
      open={presence.state === 'open'}
      onClose={onClose}
      title={t('next.sinopia.blockedTitle')}
      lead={t('next.sinopia.blockedLead')}
      boxClass="hx-sinopia-block"
    >
      <ul className="hx-sinopia-block__list">
        {shown.map((r) => (
          <FindingLine key={`${r.app}/${r.check}`} result={r} />
        ))}
      </ul>
      <div className="hx-sinopia-block__actions">
        <Button variant="primary" onClick={onClose}>
          {t('next.sinopia.blockedOk')}
        </Button>
      </div>
    </Modal>
  );
};

export default SinopiaStage;
