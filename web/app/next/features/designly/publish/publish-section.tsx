// «Опубликовать как сайт», the last section of «Поделиться» where the design may be published: one row, folding open
// like a link's. Before: «Сайт не опубликован · Опубликовать»; after: the address with «Копировать» and «Открыть».
// Unfolded: the site's name, «Кто может открыть» (two cards), the pages and «Опубликовать» / «Обновить». On the phone
// the dialog pushes the same form as a view of its own instead of unfolding it (`onPush`).
import { useId, useState, type RefObject } from 'react';
import { copyText } from '@/next/features/designly/access/copy-link';
import { PagesPicker } from '@/next/features/designly/access/link-scope';
import type { TSiteAccess } from '@/next/features/designly/publish/publish-api';
import type { IPublishState } from '@/next/features/designly/publish/use-publish';
import Icon from '@/next/ui/icon';
import { useSeen } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const ACCESS: readonly TSiteAccess[] = ['team', 'link'];

/** The fields and the button: the same in the fold and in the phone's view. */
export const PublishForm = ({
  state,
  inputRef,
}: {
  state: IPublishState;
  inputRef?: RefObject<HTMLInputElement | null>;
}) => {
  const nameId = useId();
  const { name, valid, busy } = state;
  return (
    <form
      className="hx-acc-stack hx-pub-form"
      data-section="publish-form"
      onSubmit={(event) => {
        event.preventDefault();
        void state.run();
      }}
    >
      <label className="hx-acc-sec__title" htmlFor={nameId}>
        {t('next.publish.name')}
      </label>
      <div className="hx-acc-field" data-bad={name !== '' && !valid ? '' : undefined}>
        <input
          id={nameId}
          ref={inputRef}
          className="hx-acc-field__in"
          value={name}
          maxLength={30}
          autoComplete="off"
          spellCheck={false}
          disabled={busy}
          placeholder={t('next.publish.namePlaceholder')}
          onChange={(event) => state.setName(event.target.value)}
        />
      </div>
      {name !== '' && !valid && <p className="hx-create__err">{t('next.publish.nameBad')}</p>}
      <span className="hx-acc-sec__title">{t('next.publish.access')}</span>
      <div className="hx-pub-cards" role="radiogroup" aria-label={t('next.publish.access')}>
        {ACCESS.map((value) => (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={state.access === value}
            className="hx-pub-card"
            disabled={busy}
            onClick={() => state.setAccess(value)}
          >
            <span className="hx-pub-card__title">{t(`next.publish.access.${value}`)}</span>
            <span className="hx-pub-card__note">
              {value === 'team' ? t('next.publish.teamNote') : t('next.publish.linkNote')}
            </span>
          </button>
        ))}
      </div>
      {state.pages.length > 1 && (
        <PagesPicker pages={state.pages} value={state.chosen} onChange={state.setChosen} disabled={busy} />
      )}
      {state.error && (
        <p className="hx-create__err" role="alert">
          {state.error}
        </p>
      )}
      <div className="hx-pub-form__go">
        <button
          type="submit"
          className="hx-btn"
          data-variant="primary"
          data-size="sm"
          data-busy={busy}
          disabled={busy || !valid}
        >
          <span>{state.done ? t('next.publish.update') : t('next.publish.go')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </div>
    </form>
  );
};

const PublishSection = ({ state, onPush }: { state: IPublishState; onPush?: () => void }) => {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  const seen = useSeen(open);
  const foldId = useId();
  const done = state.done;
  const toggle = () => (onPush ? onPush() : setOpen((now) => !now));
  const copy = () => {
    if (!done) return;
    setCopyFailed(false);
    copyText(Promise.resolve(done.url)).then(
      () => setCopied(true),
      () => setCopyFailed(true),
    );
  };
  return (
    <section className="hx-acc-sec" data-section="publish">
      <h3 className="hx-acc-sec__title">{t('next.publish.section')}</h3>
      <div className="hx-acc-row hx-pub-row">
        <span className="hx-acc-av" aria-hidden="true">
          <Icon name="globe" size="sm" />
        </span>
        <span className="hx-acc-row__who">
          {done ? (
            <>
              <span className="hx-acc-row__nm hx-pub-row__url">{done.url}</span>
              <span className="hx-acc-row__sub">
                {state.access === 'team' ? t('next.publish.access.team') : t('next.publish.access.link')}
              </span>
            </>
          ) : (
            <span className="hx-acc-row__sub">{t('next.publish.none')}</span>
          )}
        </span>
        {done && (
          <>
            <button type="button" className="hx-btn" data-variant="ghost" data-size="sm" onClick={copy}>
              <Icon name={copied ? 'check' : 'link'} size="sm" />
              <span>{copied ? t('next.publish.copied') : t('next.publish.copy')}</span>
            </button>
            <a
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              href={done.url}
              target="_blank"
              rel="noopener noreferrer"
            >
              <Icon name="external-link" size="sm" />
              <span>{t('next.publish.open')}</span>
            </a>
          </>
        )}
        <button
          type="button"
          className={done ? 'hx-head-btn hx-pub-row__more' : 'hx-btn'}
          data-variant={done ? undefined : 'ghost'}
          data-size={done ? undefined : 'sm'}
          aria-expanded={onPush ? undefined : open}
          aria-controls={onPush ? undefined : foldId}
          aria-label={done ? t('next.publish.settings') : undefined}
          onClick={toggle}
        >
          {done ? (
            <Icon name={onPush ? 'chevron-right' : 'chevron-down'} size="sm" />
          ) : (
            <span>{t('next.publish.go')}</span>
          )}
        </button>
      </div>
      {copyFailed && <p className="hx-create__err">{t('next.site.copyFailed')}</p>}
      {!onPush && (
        <div id={foldId} className="hx-acc-fold" data-open={open}>
          <div className="hx-acc-fold__in" inert={!open}>
            {seen && <PublishForm state={state} />}
          </div>
        </div>
      )}
    </section>
  );
};

export default PublishSection;
