import { useState } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { menuPointOf, wantsSystemMenu, type IMenuPoint } from '@/next/ui/context-menu';
import ProjectMenu from '@/next/features/sidebar/project-menu';

export interface IProjectHeadProps {
  id: string;
  name: string;
  open: boolean;
  onToggle: () => void;
  onNewChat?: () => void;
  onLook: () => void;
  onSettings?: () => void;
}

/**
 * A project's head: the folder and the name over almost the whole row, «⋯» at the end. The row's buttons (new chat, look,
 * settings) live in the menu, which a right click or two fingers open as well, as on a chat.
 */
const ProjectHead = ({ id, name, open, onToggle, onNewChat, onLook, onSettings }: IProjectHeadProps) => {
  const [mounted, setMounted] = useState(false);
  const [at, setAt] = useState<IMenuPoint | null>(null);
  const [held, setHeld] = useState(false);

  return (
    <div
      className="hx-item hx-project"
      data-drop="project"
      data-drop-id={id}
      data-drop-box="parent"
      data-menu={held || undefined}
      onContextMenu={(event) => {
        if (event.defaultPrevented || wantsSystemMenu(event.target, false)) return;
        event.preventDefault();
        if ((event.nativeEvent as Partial<PointerEvent>).pointerType === 'touch') return;
        setAt(menuPointOf(event));
        setMounted(true);
        setHeld(true);
      }}
    >
      <button
        type="button"
        className="hx-project__open"
        aria-expanded={open}
        data-tip={name}
        onClick={onToggle}
      >
        <span className="hx-project__mark" aria-hidden="true">
          <Icon name={open ? 'folder-open' : 'folder'} className="hx-project__icon" />
          <span className="hx-mono">{name.slice(0, 1).toUpperCase()}</span>
        </span>
        <span className="hx-item__title">{name}</span>
      </button>
      <span className="hx-item__end">
        {!mounted && (
          <button
            type="button"
            className="hx-item__more hx-item__menu"
            aria-label={t('next.rowMenu.label')}
            aria-haspopup="menu"
            onClick={() => {
              setMounted(true);
              setHeld(true);
            }}
          >
            <Icon name="ellipsis" size="sm" />
          </button>
        )}
        {mounted && (
          <ProjectMenu
            key={at ? `at:${at.x}:${at.y}` : 'more'}
            at={at}
            name={name}
            defaultOpen
            onOpenChange={setHeld}
            onClosed={() => {
              setAt(null);
              setMounted(false);
            }}
            trigger={
              <button
                type="button"
                className="hx-item__more hx-item__menu"
                aria-label={t('next.rowMenu.label')}
                aria-haspopup="menu"
              >
                <Icon name="ellipsis" size="sm" />
              </button>
            }
            onNewChat={onNewChat}
            onLook={onLook}
            onSettings={onSettings}
          />
        )}
      </span>
    </div>
  );
};

export default ProjectHead;
