// The machine the start page's pane opens its tools on, for a person with several: a quiet control at the end of the
// «Машина …» label, a menu of the machines (one that does not answer says why and cannot be picked).
import { Menu } from '@base-ui/react/menu';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import type { IMachineChoice } from '@/next/features/sidebar/types';
import { t } from '@/next/i18n';

export interface IMachinePickProps {
  current: string;
  choices: readonly IMachineChoice[];
  onPick: (id: string) => void;
}

const MachinePick = ({ current, choices, onPick }: IMachinePickProps) => {
  const actions = useMenuActions();
  const label = t('next.review.pickMachine');
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        render={
          <button type="button" className="hx-tl__pick" aria-label={label}>
            <Icon name="chevrons-up-down" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" menu="machine">
        {choices.map((m) => (
          <Menu.Item
            key={m.id}
            className={m.note ? 'hx-cmenu__item hx-cmenu__item--two' : 'hx-cmenu__item'}
            role="menuitemradio"
            aria-checked={m.id === current}
            disabled={m.off}
            onClick={() => onPick(m.id)}
          >
            <Icon name="cpu" size="sm" />
            <span className="hx-cmenu__txt">
              {m.name}
              {m.note && <span className="hx-cmenu__hint">{m.note}</span>}
            </span>
            <Icon name="check" size="sm" className="hx-cmenu__check" />
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};

export default MachinePick;
