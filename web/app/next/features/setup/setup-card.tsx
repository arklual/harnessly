import { Fragment } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { MARK, type ICard, type TAction } from '@/next/features/setup/card-model';

export interface ISetupCardProps {
  card: ICard;
  /** The card is leaving (hidden, or the setup is done): it fades out before it goes. */
  closing: boolean;
  /** An action of the line is being sent. */
  busy: boolean;
  onPick: (stage: number) => void;
  onAction: (kind: TAction) => void;
  onHide: () => void;
}

/** The start page's «Настройка машины»: the stages of the team's environment on this machine, as on the board «Главная». */
const SetupCard = ({ card, closing, busy, onPick, onAction, onHide }: ISetupCardProps) => (
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
            {card.more > 0 && <span className="hx-inside__it">{t('next.setup.more', { n: card.more })}</span>}
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
            onClick={() => card.line.action && onAction(card.line.action.kind)}
          >
            {card.line.action.label}
          </button>
        )}
        <button type="button" className="hx-setup__quiet" onClick={onHide}>
          {t('next.setup.hide')}
        </button>
      </span>
    </div>
  </div>
);

export default SetupCard;
