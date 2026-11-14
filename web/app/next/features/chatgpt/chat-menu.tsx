// «⋯» of a ChatGPT chat: rename and delete, as a chat's own menu does, in the screen's bar.
import { useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';

const VIEW = { root: 'root', rename: 'rename', remove: 'remove' } as const;
type TView = (typeof VIEW)[keyof typeof VIEW];

interface IGptChatMenuProps {
  title: string;
  onRename: (title: string) => Promise<unknown>;
  onDelete: () => Promise<unknown>;
}

const GptChatMenu = ({ title, onRename, onDelete }: IGptChatMenuProps) => {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<TView>(VIEW.root);
  const [name, setName] = useState(title);
  const [busy, setBusy] = useState(false);

  const toggle = (next: boolean) => {
    setOpen(next);
    if (next) {
      setView(VIEW.root);
      setName(title);
    }
  };

  const run = (action: () => Promise<unknown>) => {
    setBusy(true);
    action().then(
      () => {
        setBusy(false);
        setOpen(false);
      },
      () => setBusy(false),
    );
  };

  return (
    <Popover
      open={open}
      onOpenChange={toggle}
      label={t('next.chatgpt.side.more')}
      className="hx-gpt-menu"
      side="bottom"
      align="end"
      trigger={
        <button type="button" className="hx-head-btn" aria-label={t('next.chatgpt.side.more')}>
          <Icon name="ellipsis" />
        </button>
      }
    >
      {view === VIEW.root && (
        <>
          <button type="button" className="hx-gpt-menu__item" onClick={() => setView(VIEW.rename)}>
            <Icon name="pencil" size="sm" />
            {t('next.chatgpt.side.rename')}
          </button>
          <button
            type="button"
            className="hx-gpt-menu__item"
            data-danger="true"
            onClick={() => setView(VIEW.remove)}
          >
            <Icon name="trash-2" size="sm" />
            {t('next.chatgpt.side.delete')}
          </button>
        </>
      )}
      {view === VIEW.rename && (
        <form
          className="hx-gpt-menu__rename"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) run(() => onRename(name.trim()));
          }}
        >
          <input
            className="hx-set__input"
            value={name}
            maxLength={200}
            aria-label={t('next.chatgpt.side.rename')}
            onChange={(e) => setName(e.target.value)}
          />
          <div className="hx-gpt-menu__row">
            <button type="button" className="hx-gpt-menu__btn" onClick={() => setView(VIEW.root)}>
              {t('next.chatgpt.side.cancel')}
            </button>
            <button type="submit" className="hx-gpt-menu__btn" disabled={busy || !name.trim()}>
              {t('next.chatgpt.side.save')}
            </button>
          </div>
        </form>
      )}
      {view === VIEW.remove && (
        <>
          <div className="hx-gpt-menu__ask">
            <b>{t('next.chatgpt.side.deleteAsk')}</b>
          </div>
          <div className="hx-gpt-menu__row">
            <button type="button" className="hx-gpt-menu__btn" onClick={() => setView(VIEW.root)}>
              {t('next.chatgpt.side.cancel')}
            </button>
            <button
              type="button"
              className="hx-gpt-menu__btn"
              data-danger="true"
              disabled={busy}
              onClick={() => run(onDelete)}
            >
              {t('next.chatgpt.side.delete')}
            </button>
          </div>
        </>
      )}
    </Popover>
  );
};

export default GptChatMenu;
