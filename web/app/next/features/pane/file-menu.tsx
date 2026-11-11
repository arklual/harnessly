// «…» of a file or a folder: one set of actions wherever a file shows — its row in «Файлы» and the pane's
// header over an open file. A menu beside the control on a wide window, a bottom sheet on the phone (MenuPanel).
import { Menu } from '@base-ui/react/menu';
import type { ReactElement } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { downloadHref, saveFrom } from '@/next/features/pane/download';
import { previewUrl } from '@/next/features/pane/delivered';
import { addQuote } from '@/next/stores/quotes';
import { isChatPlace } from '@/next/features/pane/place';

/** What opens as a page of its own in a new tab of the browser: the runtime's viewer shows these as they are. */
const IN_TAB = /\.(?:png|jpe?g|gif|webp|svg|avif|bmp|ico|pdf|html?|mp4|m4v|webm|mov|mp3|wav|m4a|ogg|flac)$/i;

export interface IFileMenuProps {
  trigger: ReactElement;
  machine: MachineId;
  conv: ConvId;
  /** As the chat's folder names it: relative to it, or absolute. */
  path: string;
  /** The absolute path: what is copied and what the agent is told. */
  abs: string;
  kind: 'file' | 'directory';
  side?: 'bottom' | 'top';
  align?: 'start' | 'end';
  /** «Сравнить с git»: set where the file can be shown against its last commit. */
  onCompare?: () => void;
  /** «Открыть в новой вкладке»: the entry in a tab of the pane of its own. */
  onOpenBeside?: () => void;
  /** «Переименовать» and «В корзину»: set where the entry can be changed (the list of «Файлы»). */
  onRename?: () => void;
  onTrash?: () => void;
}

const FileMenu = ({
  trigger,
  machine,
  conv,
  path,
  abs,
  kind,
  side = 'bottom',
  align = 'end',
  onCompare,
  onOpenBeside,
  onRename,
  onTrash,
}: IFileMenuProps) => {
  const actions = useMenuActions();
  const folder = kind === 'directory';
  const copy = () =>
    void navigator.clipboard.writeText(abs).then(
      () => toast(t('next.fileMenu.copied'), { tone: 'success' }),
      () => toast(t('next.fileMenu.copyFailed'), { tone: 'error' }),
    );
  const ask = () => {
    addQuote(conv, abs, null);
    toast(t('next.fileMenu.asked'), { tone: 'success' });
  };
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger render={trigger} />
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={t('next.fileMenu.label')}
        side={side}
        align={align}
        sideOffset={6}
      >
        <Menu.Item className="hx-cmenu__item" onClick={() => saveFrom(downloadHref(machine, conv, [path]))}>
          <Icon name="download" size="sm" />
          <span className="hx-cmenu__txt">
            {t(folder ? 'next.fileMenu.downloadZip' : 'next.fileMenu.download')}
          </span>
        </Menu.Item>
        {onOpenBeside && (
          <Menu.Item className="hx-cmenu__item" onClick={onOpenBeside}>
            <Icon name="app-window" size="sm" />
            <span className="hx-cmenu__txt">{t('next.fileMenu.openBeside')}</span>
          </Menu.Item>
        )}
        {!folder && IN_TAB.test(abs) && (
          <Menu.Item
            className="hx-cmenu__item"
            onClick={() => window.open(previewUrl(machine, abs), '_blank', 'noopener')}
          >
            <Icon name="external-link" size="sm" />
            <span className="hx-cmenu__txt">{t('next.fileMenu.openInTab')}</span>
          </Menu.Item>
        )}
        {onCompare && (
          <Menu.Item className="hx-cmenu__item" onClick={onCompare}>
            <Icon name="git-compare" size="sm" />
            <span className="hx-cmenu__txt">{t('next.fileMenu.compare')}</span>
          </Menu.Item>
        )}
        <Menu.Separator className="hx-cmenu__sep" />
        {/* The start page's files have no chat to ask: the item is there only beside one. */}
        {isChatPlace({ id: conv }) && (
          <Menu.Item className="hx-cmenu__item" onClick={ask}>
            <Icon name="message-square-text" size="sm" />
            <span className="hx-cmenu__txt">{t('next.fileMenu.ask')}</span>
          </Menu.Item>
        )}
        <Menu.Item className="hx-cmenu__item" onClick={copy}>
          <Icon name="copy" size="sm" />
          <span className="hx-cmenu__txt">{t('next.fileMenu.copyPath')}</span>
        </Menu.Item>
        {(onRename || onTrash) && <Menu.Separator className="hx-cmenu__sep" />}
        {onRename && (
          <Menu.Item className="hx-cmenu__item" onClick={onRename}>
            <Icon name="pencil" size="sm" />
            <span className="hx-cmenu__txt">{t('next.fileMenu.rename')}</span>
          </Menu.Item>
        )}
        {onTrash && (
          <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={onTrash}>
            <Icon name="trash-2" size="sm" />
            <span className="hx-cmenu__txt">{t('next.fileMenu.trash')}</span>
          </Menu.Item>
        )}
      </MenuPanel>
    </Menu.Root>
  );
};

export default FileMenu;
