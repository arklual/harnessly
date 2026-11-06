import { Fragment, useState, type FormEvent } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { ACTION, MARK, type ICard, type TAction } from '@/next/features/setup/card-model';

export interface ISetupCardProps {
  card: ICard;
  /** The card is leaving (hidden, or the setup is done): it fades out before it goes. */
  closing: boolean;
  /** An action of the line is being sent. */
  busy: boolean;
  onPick: (stage: number) => void;
  onAction: (kind: TAction) => void;
  /** «Указать прокси» sent an address; resolves false when it did not fit (the form stays). */
  onProxy: (url: string) => Promise<boolean>;
  /** The proxy kept now ('' for none), to start the form with. */
  proxy: string;
  onHide: () => void;
}

const ProxyForm = ({
  start,
  onSave,
  onClose,
}: {
  start: string;
  onSave: (url: string) => Promise<boolean>;
  onClose: () => void;
}) => {
  const [url, setUrl] = useState(start);
  const [sending, setSending] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (sending) return;
    setSending(true);
    const ok = await onSave(url.trim());
    setSending(false);
    if (ok) onClose();
  };
  return (
    <form className="hx-setup__proxy" onSubmit={(e) => void submit(e)}>
      <label className="hx-setup__proxyl">
        <span>{t('next.setup.proxy.label')}</span>
        <input
          autoFocus
          value={url}
          placeholder={t('next.setup.proxy.placeholder')}
          disabled={sending}
          onChange={(e) => setUrl(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') onClose();
          }}
        />
      </label>
      <small className="hx-setup__rest">{t('next.setup.proxy.hint')}</small>
      <span className="hx-setup__acts">
        <button type="submit" className="hx-setup__pill" data-main="true" disabled={sending}>
          {t('next.setup.proxy.save')}
        </button>
        <button type="button" className="hx-setup__quiet" onClick={onClose}>
          {t('next.setup.proxy.cancel')}
        </button>
      </span>
    </form>
  );
};

/** The start page's «Настройка машины»: the stages of the team's environment on this machine, as on the board «Главная». */
const SetupCard = ({ card, closing, busy, onPick, onAction, onProxy, proxy, onHide }: ISetupCardProps) => {
  const [proxyOpen, setProxyOpen] = useState(false);
  const act = (kind: TAction) => (kind === ACTION.proxy ? setProxyOpen(true) : onAction(kind));
  return (
    <div className="hx-setup" data-closing={closing ? 'true' : 'false'}>
      <div className="hx-setup__top">
        <Icon name="laptop" />
        <span className="hx-setup__title">{card.title}</span>
      </div>
      {card.stages.length > 0 && !card.done && (
        <>
          <div className="hx-chain" role="list" aria-label={t('next.setup.chain')}>
            {card.stages.map((s, i) => (
              <Fragment key={s.name}>
                {i > 0 && <span className="hx-chain__link" data-s={s.linkDone ? MARK.done : MARK.wait} />}
                <button
                  type="button"
                  className="hx-chain__node"
                  role="listitem"
                  data-s={s.mark}
                  data-pick={i === card.shown && i !== card.current ? 'true' : 'false'}
                  data-cur={i === card.current ? 'true' : 'false'}
                  onClick={() => onPick(i)}
                >
                  <span className="hx-chain__dot">
                    {(s.mark === MARK.done || s.mark === MARK.fail) && (
                      <Icon name={s.mark === MARK.fail ? 'x' : 'check'} />
                    )}
                  </span>
                  <span className="hx-chain__label">{s.name}</span>
                  {s.count && <span className="hx-chain__n">{s.count}</span>}
                </button>
              </Fragment>
            ))}
          </div>
          {card.steps.length > 0 && (
            <div className="hx-inside">
              {card.steps.map((x, k) => (
                <span key={x.name} className="hx-inside__it" data-s={x.mark} style={{ ['--k' as string]: k }}>
                  <span className="hx-inside__m">
                    {x.mark === MARK.done && <Icon name="check" />}
                    {x.mark === MARK.fail && <Icon name="x" />}
                  </span>
                  {x.name}
                </span>
              ))}
              {card.more > 0 && (
                <span className="hx-inside__it">{t('next.setup.more', { n: card.more })}</span>
              )}
            </div>
          )}
        </>
      )}
      {card.run && (
        <div className="hx-setup__run">
          <div className="hx-setup__runrow">
            <span className="hx-setup__runtxt">{card.run.text}</span>
            <span className="hx-setup__runpct">{card.run.count}</span>
          </div>
          <div className="hx-setup__bar">
            <i style={{ width: card.run.width }} />
          </div>
        </div>
      )}
      <div className="hx-setup__line" data-s={card.line.mark}>
        <span className="hx-setup__say">
          {card.line.title && (
            <b>
              {card.line.spin && <span className="hx-setup__spin" aria-hidden="true" />}
              {card.line.title}
            </b>
          )}
          {card.line.rest && <span className="hx-setup__rest">{card.line.rest}</span>}
        </span>
        <span className="hx-setup__acts">
          {card.line.action && (
            <button
              type="button"
              className="hx-setup__pill"
              data-main="true"
              disabled={busy}
              onClick={() => card.line.action && act(card.line.action.kind)}
            >
              {card.line.action.label}
            </button>
          )}
          {card.line.extra.map((x) => (
            <button
              key={x.kind}
              type="button"
              className="hx-setup__pill"
              disabled={busy}
              onClick={() => act(x.kind)}
            >
              {x.label}
            </button>
          ))}
          <button type="button" className="hx-setup__quiet" onClick={onHide}>
            {t('next.setup.hide')}
          </button>
        </span>
      </div>
      {proxyOpen && <ProxyForm start={proxy} onSave={onProxy} onClose={() => setProxyOpen(false)} />}
    </div>
  );
};

export default SetupCard;
