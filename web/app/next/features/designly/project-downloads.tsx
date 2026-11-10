// «Скачать» of a Sinopia project: one menu wherever a design, a deck or a video shows — the bar's own button,
// «Ещё», a page's row in the list of pages and its tab — in the right pane, the full view and the editing one; on a
// phone it is a bottom sheet (MenuPanel). Each download is a job of the machine (deck/exports.ts) that ends in the
// browser's download. The whole project as a ZIP is for those who may change it.
import { Menu } from '@base-ui/react/menu';
import { Fragment, type ReactElement } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import type { MachineId } from '@/next/protocol/v2';
import { runDeckExport } from '@/next/features/designly/deck/exports';
import { DECK_EXPORT, type TDeckExport } from '@/next/features/designly/deck/types';

export interface IProjectDownloadsOf {
  machine: MachineId;
  artifact: string;
  /** The page on screen (its board's path or id); null where there is none. */
  board: string | null;
  /** The whole project as a ZIP: set where the person may change it. */
  archives: boolean;
}

export interface IDownloadItem {
  id: string;
  icon: string;
  label: string;
  /** A second line: what the file is, or why it cannot be made now. */
  hint?: string;
  disabled?: boolean;
  /** A separator above it. */
  sep?: boolean;
  pick: () => void;
}

/** What a design's page or a video's scene is called in the menu. */
export const DOWNLOAD_UNIT = { page: 'page', scene: 'scene' } as const;
export type TDownloadUnit = (typeof DOWNLOAD_UNIT)[keyof typeof DOWNLOAD_UNIT];

const LABELS = {
  page: {
    pdf: 'next.download.pagePdf',
    html: 'next.download.pageHtml',
    png: 'next.download.pagePng',
    all: 'next.download.allPdf',
  },
  scene: {
    pdf: 'next.download.scenePdf',
    html: 'next.download.sceneHtml',
    png: 'next.download.scenePng',
    all: 'next.download.allScenesPdf',
  },
} as const satisfies Record<TDownloadUnit, Record<'pdf' | 'html' | 'png' | 'all', string>>;

const run = (of: IProjectDownloadsOf, format: TDeckExport, board?: string) =>
  void runDeckExport(of.machine, of.artifact, format, { board }).catch(() => undefined);

/** The downloads of one page (or scene): PDF, one HTML file, a picture. */
export const pageDownloads = (
  of: IProjectDownloadsOf,
  board: string,
  unit: TDownloadUnit,
): IDownloadItem[] => [
  {
    id: 'page-pdf',
    icon: 'file-text',
    label: t(LABELS[unit].pdf),
    pick: () => run(of, DECK_EXPORT.pdf, board),
  },
  {
    id: 'page-html',
    icon: 'file-code',
    label: t(LABELS[unit].html),
    pick: () => run(of, DECK_EXPORT.html, board),
  },
  { id: 'page-png', icon: 'image', label: t(LABELS[unit].png), pick: () => run(of, DECK_EXPORT.png, board) },
];

/** The whole project as a ZIP: the pages, notes and comments with the boards. */
export const bundleDownload = (of: IProjectDownloadsOf): IDownloadItem => ({
  id: 'bundle',
  icon: 'files',
  label: t('next.download.bundle'),
  sep: true,
  pick: () => run(of, DECK_EXPORT.bundle),
});

/** A design's or a video's downloads, in the menu's order: the page on screen, every page, the project. */
export const projectDownloads = (
  of: IProjectDownloadsOf,
  unit: TDownloadUnit = DOWNLOAD_UNIT.page,
): IDownloadItem[] => {
  const items: IDownloadItem[] = of.board === null ? [] : pageDownloads(of, of.board, unit);
  items.push({
    id: 'all-pdf',
    icon: 'files',
    label: t(LABELS[unit].all),
    sep: items.length > 0,
    pick: () => run(of, DECK_EXPORT.pdf),
  });
  if (of.archives) items.push(bundleDownload(of));
  return items;
};

export interface IProjectDownloadMenuProps {
  items: readonly IDownloadItem[];
  /** The control it opens from; absent for a menu opened at a point (`anchor`, `open`). */
  trigger?: ReactElement;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  anchor?: { getBoundingClientRect: () => DOMRect };
  side?: 'bottom' | 'top';
  align?: 'start' | 'end';
}

/** The downloads as a menu: beside its control on a wide window, a bottom sheet on the phone. */
export const ProjectDownloadMenu = ({
  items,
  trigger,
  open,
  onOpenChange,
  anchor,
  side = 'bottom',
  align = 'end',
}: IProjectDownloadMenuProps) => {
  const actions = useMenuActions();
  return (
    <Menu.Root actionsRef={actions} open={open} onOpenChange={onOpenChange}>
      {trigger && <Menu.Trigger render={trigger} />}
      <MenuPanel
        actions={actions}
        className="hx-cmenu hx-dlmenu"
        label={t('next.download.label')}
        side={side}
        align={align}
        sideOffset={6}
        anchor={anchor}
      >
        {items.map((item) => (
          <Fragment key={item.id}>
            {item.sep && <Menu.Separator className="hx-cmenu__sep" />}
            <Menu.Item
              className={item.hint ? 'hx-cmenu__item hx-cmenu__item--two' : 'hx-cmenu__item'}
              disabled={item.disabled}
              onClick={item.pick}
            >
              <Icon name={item.icon} size="sm" />
              <span className="hx-cmenu__txt">
                {item.label}
                {item.hint && <span className="hx-cmenu__hint">{item.hint}</span>}
              </span>
            </Menu.Item>
          </Fragment>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};
