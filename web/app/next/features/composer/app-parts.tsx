// What the apps on in a chat put into the chat's own interface (chat.buttons, chat.starters of their manifests): the
// buttons at the field and the lines an empty chat offers. A button runs one of its app's commands (it goes into the
// field, as the «/» palette puts it) or opens the app's view in the pane.
import { Menu } from '@base-ui/react/menu';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { openAppTab } from '@/next/features/app-views/open';
import { t } from '@/next/i18n';
import { RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';

/** Buttons that stand at the field; the rest go into one «⋯» menu. */
const INLINE_BUTTONS = 2;
/** Lines an empty chat offers, all apps together. */
const STARTERS = 4;

export interface IAppButton {
  key: string;
  appName: string;
  title: string;
  icon: string;
  /** What goes into the field (`/<plugin>:<name> `), or the view opened in the pane. */
  run: { field: string } | { pane: string };
}

const pluginOf = (id: string): string => id.replaceAll('.', '-');

/** The buttons of the apps from `on` (the chat's ids, in their order). */
export const appButtonsOf = (installed: readonly IInstalledApp[], on: readonly string[]): IAppButton[] =>
  on.flatMap((id) => {
    const app = installed.find((a) => a.id === id);
    return (app?.buttons ?? []).map((b) => ({
      key: `${app?.id}/${b.id}`,
      appName: app?.name ?? id,
      title: b.title,
      icon: b.icon ?? app?.icon ?? 'layout-grid',
      run: b.command
        ? { field: `/${pluginOf(id)}:${b.command.replace(/^\//, '')} ` }
        : { pane: `${id}/${b.view ?? ''}` },
    }));
  });

/** The lines an empty chat offers: the apps' starters, in the chat's order, at most STARTERS. */
export const appStartersOf = (installed: readonly IInstalledApp[], on: readonly string[]): string[] =>
  on
    .flatMap((id) => installed.find((a) => a.id === id)?.starters?.map((s) => s.text) ?? [])
    .slice(0, STARTERS);

const press = (b: IAppButton, onField: (text: string) => void) => {
  if ('field' in b.run) onField(b.run.field);
  else openAppTab(b.run.pane);
};

const label = (b: IAppButton) => `${b.title} · ${b.appName}`;

export const AppButtons = ({
  buttons,
  onField,
}: {
  buttons: readonly IAppButton[];
  onField: (text: string) => void;
}) => {
  const actions = useMenuActions();
  if (buttons.length === 0) return null;
  // One more than fits stands at the field too: a menu of one is no saving.
  const inline = buttons.length <= INLINE_BUTTONS + 1 ? buttons : buttons.slice(0, INLINE_BUTTONS);
  const rest = buttons.slice(inline.length);
  const more = t('next.composer.appsMore');
  return (
    <>
      {inline.map((b) => (
        <RoundButton
          key={b.key}
          icon={b.icon}
          label={label(b)}
          variant="ghost"
          onClick={() => press(b, onField)}
        />
      ))}
      {rest.length > 0 && (
        <Menu.Root actionsRef={actions}>
          <Menu.Trigger
            render={
              <button type="button" className="hx-round" data-variant="ghost" aria-label={more}>
                <Icon name="ellipsis" />
              </button>
            }
          />
          <MenuPanel
            actions={actions}
            className="hx-cmenu"
            label={more}
            side="top"
            align="end"
            menu="app-buttons"
          >
            {rest.map((b) => (
              <Menu.Item key={b.key} className="hx-cmenu__item" onClick={() => press(b, onField)}>
                <Icon name={b.icon} size="sm" />
                <span className="hx-cmenu__txt">{label(b)}</span>
              </Menu.Item>
            ))}
          </MenuPanel>
        </Menu.Root>
      )}
    </>
  );
};

/** The lines an empty chat offers under its field; a click puts the line into the field, it sends nothing. */
export const AppStarters = ({
  lines,
  onPick,
}: {
  lines: readonly string[];
  onPick: (text: string) => void;
}) =>
  lines.length === 0 ? null : (
    <div className="hx-starters" role="list" aria-label={t('next.composer.starters')}>
      {lines.map((line) => (
        <button
          key={line}
          type="button"
          role="listitem"
          className="hx-starters__item"
          onClick={() => onPick(line)}
        >
          {line}
        </button>
      ))}
    </div>
  );
