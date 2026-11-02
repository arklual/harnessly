// «+» → «Приложение»: the installed apps that work in a chat and are not on here yet; a pick turns the app on in this
// chat. It sits in the «+» panel in place of the menu (a popover on a computer, the same sheet on a phone), after the
// pickers of places. The list is read when the pane opens, not with the chat: a read starts the machine's view server.
import { useInstalledApps } from '@/next/features/app-views/api';
import { offeredApps } from '@/next/features/app-main/chat-apps';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

export interface IPlusApps {
  machine: MachineId;
  /** The ids already on in the chat. */
  on: readonly string[];
  /** Turns the app on in the chat. */
  onPick: (id: string) => void;
  /** Opens the list of every installed app. */
  onAll: () => void;
}

const AppsPane = ({ apps, onBack }: { apps: IPlusApps; onBack: () => void }) => {
  const { list, error } = useInstalledApps(apps.machine);
  const offered = list === null ? [] : offeredApps(list.apps, apps.on);
  let body = null;
  if (error) {
    body = (
      <LoadFailed
        size={LOAD_FAILED_SIZE.inline}
        title={t('next.chatapps.loadFailed')}
        reason={error}
        onRetry={onBack}
      />
    );
  } else if (list === null) {
    body = (
      <div className="hx-pick__state" data-busy="true" role="status">
        <span className="hx-spin" aria-hidden="true" />
        {t('next.attach.loading')}
      </div>
    );
  } else if (offered.length === 0) {
    body = <div className="hx-pick__state">{t('next.chatapps.none')}</div>;
  } else {
    body = offered.map((app) => (
      <button
        key={app.id}
        type="button"
        className="hx-pick__row"
        role="option"
        onClick={() => apps.onPick(app.id)}
      >
        <span className="hx-pick__thumb">
          <Icon name="layout-grid" size="sm" />
        </span>
        <span className="hx-pick__text">
          <span className="hx-pick__name">{app.name}</span>
          {app.description && <span className="hx-pick__hint">{app.description}</span>}
        </span>
      </button>
    ));
  }
  return (
    <div className="hx-plus__pane hx-pick" data-sub="true" data-kind="apps">
      <div className="hx-pick__head">
        <button type="button" className="hx-pick__back" aria-label={t('next.attach.back')} onClick={onBack}>
          <Icon name="chevron-left" size="sm" />
        </button>
        <span className="hx-pick__title">{t('next.chatapps.title')}</span>
      </div>
      <div className="hx-pick__list" role="listbox" aria-label={t('next.chatapps.title')}>
        {body}
      </div>
      <button type="button" className="hx-pick__row hx-pick__row--all" onClick={apps.onAll}>
        <span className="hx-pick__thumb">
          <Icon name="layout-grid" size="sm" />
        </span>
        <span className="hx-pick__text">
          <span className="hx-pick__name">{t('next.chatapps.all')}</span>
        </span>
      </button>
    </div>
  );
};

export default AppsPane;
