// The panel of a Base UI menu (the sidebar's «…», a chat tab's menu, the profile menu): placed by the control or the
// point it opened from on a wide window, a bottom sheet on the phone (ui/sheet.tsx) with its grip, scrim and swipe down.
// The menu's Root passes `actionsRef` (useMenuActions) so the swipe can close it.
import { Menu } from '@base-ui/react/menu';
import { useRef, useState, type ReactNode, type RefObject } from 'react';
import { usePortalContainer } from '@/next/ui/portal';
import { SheetGrip, useSheetDrag } from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';

export type TMenuActions = RefObject<Menu.Root.Actions | null>;

/** What the menu's Root takes as `actionsRef`, and MenuPanel as `actions`. */
export const useMenuActions = (): TMenuActions => useRef<Menu.Root.Actions | null>(null);

interface IMenuPanelProps {
  actions: TMenuActions;
  className: string;
  label: string;
  side?: 'top' | 'bottom' | 'left' | 'right';
  align?: 'start' | 'center' | 'end';
  sideOffset?: number;
  anchor?: { getBoundingClientRect: () => DOMRect };
  finalFocus?: () => HTMLElement | boolean;
  /** data-menu / data-view of the popup, for its look. */
  menu?: string;
  view?: string;
  children: ReactNode;
}

const MenuPanel = ({
  actions,
  className,
  label,
  side = 'bottom',
  align = 'start',
  sideOffset = 4,
  anchor,
  finalFocus,
  menu,
  view,
  children,
}: IMenuPanelProps) => {
  const container = usePortalContainer();
  const phone = usePhone();
  const [popup, setPopup] = useState<HTMLDivElement | null>(null);
  useSheetDrag(popup, () => actions.current?.close(), phone);
  return (
    <Menu.Portal container={container}>
      {phone && <Menu.Backdrop className="hx-sheet-scrim hx-menu-scrim" />}
      <Menu.Positioner
        className={phone ? 'hx-cmenu-pos hx-sheet-pos' : 'hx-cmenu-pos'}
        anchor={anchor}
        side={side}
        align={align}
        sideOffset={sideOffset}
        collisionPadding={8}
      >
        <Menu.Popup
          ref={setPopup}
          className={phone ? `${className} hx-sheet-phone` : className}
          data-menu={menu}
          data-view={view}
          aria-label={label}
          finalFocus={finalFocus}
        >
          {phone && <SheetGrip />}
          {children}
        </Menu.Popup>
      </Menu.Positioner>
    </Menu.Portal>
  );
};

export default MenuPanel;
