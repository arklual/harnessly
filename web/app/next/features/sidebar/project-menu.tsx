import { Menu } from '@base-ui/react/menu';
import type { ReactElement } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { pointAnchor, type IMenuPoint } from '@/next/ui/context-menu';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { MenuItem, SheetMenu } from '@/next/ui/sheet-menu';
import { usePhone } from '@/next/ui/use-media';

export interface IProjectMenuProps {
  /** «⋯» at the end of the project's row. */
  trigger: ReactElement;
  name: string;
  /** Opened by a right click or two fingers: the menu stands at that point, not at «⋯». */
  at?: IMenuPoint | null;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** The menu has gone (its exit played). */
  onClosed?: () => void;
  onNewChat?: () => void;
  onLook?: () => void;
  onSettings?: () => void;
}

/** The menu of a project's row, as the chat's: what the row used to carry as three buttons (a new chat, the look, the settings). */
const ProjectMenu = ({
  trigger,
  name,
  at = null,
  defaultOpen = false,
  onOpenChange,
  onClosed,
  onNewChat,
  onLook,
  onSettings,
}: IProjectMenuProps) => {
  const menuActions = useMenuActions();
  const phone = usePhone();

  const items = (
    <>
      {onNewChat && (
        <MenuItem className="hx-cmenu__item" onClick={onNewChat}>
          <Icon name="plus" size="sm" />
          <span className="hx-cmenu__txt">{t('next.sidebar.newChatIn', { project: name })}</span>
        </MenuItem>
      )}
      {onLook && (
        <MenuItem className="hx-cmenu__item" onClick={onLook}>
          <Icon name="palette" size="sm" />
          <span className="hx-cmenu__txt">{t('next.rowMenu.look')}</span>
        </MenuItem>
      )}
      {onSettings && (
        <MenuItem className="hx-cmenu__item" onClick={onSettings}>
          <Icon name="settings" size="sm" />
          <span className="hx-cmenu__txt">{t('next.sidebar.projectSettings', { project: name })}</span>
        </MenuItem>
      )}
    </>
  );

  if (phone)
    return (
      <SheetMenu
        trigger={trigger}
        label={t('next.rowMenu.label')}
        className="hx-cmenu hx-sheet-phone"
        view="root"
        defaultOpen={defaultOpen}
        onOpenChange={onOpenChange}
        onClosed={onClosed}
      >
        {items}
      </SheetMenu>
    );

  return (
    <MenuRoot
      actionsRef={menuActions}
      defaultOpen={defaultOpen}
      onOpenChange={(open) => onOpenChange?.(open)}
      onOpenChangeComplete={(open) => {
        if (!open) onClosed?.();
      }}
    >
      <Menu.Trigger render={trigger} />
      <MenuPanel
        actions={menuActions}
        className="hx-cmenu"
        label={t('next.rowMenu.label')}
        view="root"
        side={at ? 'bottom' : 'bottom'}
        align={at ? 'start' : 'end'}
        sideOffset={at ? 2 : 6}
        anchor={at ? pointAnchor(at) : undefined}
      >
        {items}
      </MenuPanel>
    </MenuRoot>
  );
};

export default ProjectMenu;
