// «Опубликовать» of a design's header: the pages go to the hub as a site that opens with this machine off. Name (from the
// design's title), who may open it, the pages (chosen as for a link without sign-in), and then the address to copy or
// open. Pressing waits for the hub and says what it answered.
import { useRef, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import { copyText } from '@/next/features/designly/access/copy-link';
import { PagesPicker, type IScopePage } from '@/next/features/designly/access/link-scope';
import {
  publishDesign,
  siteNameOf,
  validSiteName,
  type IPublished,
  type TSiteAccess,
} from '@/next/features/designly/publish/publish-api';
import Modal from '@/next/features/shell/modal';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

export interface IPublishDialogProps {
  open: boolean;
  onClose: () => void;
  machine: MachineId;
  project: string;
  title: string;
  pages: readonly IScopePage[];
}

const ACCESS: readonly TSiteAccess[] = ['team', 'link'];

const PublishDialog = ({ open, onClose, machine, project, title, pages }: IPublishDialogProps) => {
  const field = useRef<HTMLInputElement>(null);
  const [name, setName] = useState(() => siteNameOf(title));
  const [access, setAccess] = useState<TSiteAccess>('team');
  const [chosen, setChosen] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<IPublished | null>(null);
  const [copied, setCopied] = useState(false);

  // The dialog opens again on the form, not on the last result (adjusted while rendering).
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setDone(null);
      setError(null);
      setCopied(false);
    }
  }

  const valid = validSiteName(name);
  const publish = async () => {
    if (busy || !valid) return;
    setBusy(true);
    setError(null);
    try {
      setDone(await publishDesign(machine, { project, name, access, pages: chosen }));
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.failure.http'));
    } finally {
      setBusy(false);
    }
  };

  const copy = () => {
    if (!done) return;
    copyText(Promise.resolve(done.url)).then(
      () => setCopied(true),
      () => setError(t('next.site.copyFailed')),
    );
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      boxClass="hx-acc"
      title={t('next.publish.title', { name: title || t('next.access.untitled') })}
      initialFocus={done ? undefined : field}
    >
      <div className="hx-modal__body hx-acc-body">
        {done ? (
          <section className="hx-acc-sec">
            <h3 className="hx-acc-sec__title">{t('next.publish.done')}</h3>
            <p className="hx-acc-row__sub">{done.url}</p>
            <p className="hx-acc-row__sub">
              {access === 'team' ? t('next.publish.teamNote') : t('next.publish.linkNote')}
            </p>
          </section>
        ) : (
          <>
            <section className="hx-acc-sec">
              <h3 className="hx-acc-sec__title">{t('next.publish.name')}</h3>
              <div className="hx-acc-field" data-bad={name !== '' && !valid ? '' : undefined}>
                <input
                  ref={field}
                  className="hx-acc-field__in"
                  value={name}
                  maxLength={30}
                  autoComplete="off"
                  spellCheck={false}
                  aria-label={t('next.publish.name')}
                  placeholder={t('next.publish.namePlaceholder')}
                  onChange={(event) => setName(event.target.value.toLowerCase())}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') void publish();
                  }}
                />
              </div>
              {name !== '' && !valid && <p className="hx-create__err">{t('next.publish.nameBad')}</p>}
            </section>
            <section className="hx-acc-sec">
              <h3 className="hx-acc-sec__title">{t('next.publish.access')}</h3>
              <Segmented
                label={t('next.publish.access')}
                value={access}
                disabled={busy}
                options={ACCESS.map((value) => ({ value, label: t(`next.publish.access.${value}`) }))}
                onChange={setAccess}
              />
              <p className="hx-acc-row__sub">
                {access === 'team' ? t('next.publish.teamNote') : t('next.publish.linkNote')}
              </p>
            </section>
            {pages.length > 1 && (
              <section className="hx-acc-sec">
                <h3 className="hx-acc-sec__title">{t('next.access.pages')}</h3>
                <PagesPicker pages={pages} value={chosen} onChange={setChosen} disabled={busy} />
              </section>
            )}
          </>
        )}
        {error && (
          <p className="hx-create__err" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot hx-acc-foot">
        {done ? (
          <>
            <div className="hx-acc-foot__left">
              <button type="button" className="hx-btn" data-variant="ghost" data-size="sm" onClick={copy}>
                <Icon name={copied ? 'check' : 'link'} size="sm" />
                <span>{copied ? t('next.publish.copied') : t('next.publish.copy')}</span>
              </button>
            </div>
            <a
              className="hx-btn"
              data-variant="primary"
              data-size="sm"
              href={done.url}
              target="_blank"
              rel="noopener noreferrer"
            >
              <span>{t('next.publish.open')}</span>
            </a>
          </>
        ) : (
          <>
            <span />
            <button
              type="button"
              className="hx-btn"
              data-variant="primary"
              data-size="sm"
              data-busy={busy}
              disabled={busy || !valid}
              onClick={() => void publish()}
            >
              <span>{t('next.publish.go')}</span>
              <span className="hx-gate__spin" aria-hidden="true" />
            </button>
          </>
        )}
      </div>
    </Modal>
  );
};

export default PublishDialog;
