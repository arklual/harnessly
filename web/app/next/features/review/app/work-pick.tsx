// Which worktree of a chat's work is reviewed, at the head of the «Ревью» panel: the chat and the chats it delegated may
// have worked in several repositories and branches. A menu, not a native select.
import { Menu } from '@base-ui/react/menu';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { t } from '@/next/i18n';

export interface IWorkOption {
  key: string;
  /** The worktree's folder name. */
  name: string;
  branch: string | null;
  /** `!12` when the branch has a merge request. */
  mr: string | null;
  /** The work of a chat this one delegated to. */
  delegated: boolean;
}

interface IWorkPickProps {
  options: readonly IWorkOption[];
  current: string;
  onPick: (key: string) => void;
}

const Label = ({ option }: { option: IWorkOption }) => (
  <span className="hx-cmenu__txt">
    {option.branch ?? option.name}
    <span className="hx-cmenu__hint">
      {[option.name, option.mr, option.delegated ? t('next.reviewApp.delegated') : null]
        .filter(Boolean)
        .join(' · ')}
    </span>
  </span>
);

export const WorkPick = ({ options, current, onPick }: IWorkPickProps) => {
  const actions = useMenuActions();
  const shown = options.find((o) => o.key === current) ?? options[0];
  if (!shown) return null;
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger className="hx-rvapp__pick">
        <Icon name="git-branch" size="sm" />
        <span className="hx-rvapp__pick-name">{shown.branch ?? shown.name}</span>
        <span className="hx-rvapp__pick-count">{t('next.reviewApp.worktrees', { n: options.length })}</span>
        <Icon name="chevron-down" size="sm" />
      </Menu.Trigger>
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={t('next.reviewApp.pickWork')}
        side="bottom"
        align="start"
      >
        {options.map((option) => (
          <Menu.Item
            key={option.key}
            className="hx-cmenu__item hx-cmenu__item--two"
            data-selected={option.key === shown.key || undefined}
            onClick={() => onPick(option.key)}
          >
            <Label option={option} />
            {option.key === shown.key && <Icon name="check" size="sm" />}
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};
