// What a device adds around the page of an open design: the phone's own interface in a case (the status bar, the browser's
// bars), «Обзор» with all sizes side by side, and the phone's code. They are drawings of the design's .hx-vp, .hx-ovw
// and .hx-phone; the page itself is the frame the site view puts between them.
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import type { IBrowser, IOverviewCard } from '@/next/features/designly/devices';

interface IBarsProps {
  browser: IBrowser;
  /** The host the browser's address shows. */
  host: string;
  /** A press on a folded bar unfolds the bars. */
  onUnfold: () => void;
}

/** Over the page: the status bar and, for a browser with its address on top, that bar. */
export const PhoneTop = ({ browser, host, onUnfold }: IBarsProps) => (
  <>
    <div className="hx-pbar" aria-hidden="true">
      <span className="hx-pbar__time">9:41</span>
      <span className="hx-spacer" />
      <span className="hx-pbar__sig">
        <i />
        <i />
        <i />
        <i />
      </span>
      <Icon name="wifi" size="sm" className="hx-pbar__wifi" />
      <span className="hx-pbar__bat">
        <i />
      </span>
    </div>
    {browser.top && (
      <div className="hx-pchrome" aria-hidden="true" onClick={onUnfold}>
        <span className="hx-pchrome__url">
          <Icon name="lock" size="sm" />
          <span className="hx-pchrome__host">{host}</span>
        </span>
        <span className="hx-pchrome__tabs">2</span>
        <Icon name="ellipsis" size="sm" className="hx-pchrome__menu" />
      </div>
    )}
  </>
);

const TOOLS = ['chevron-left', 'chevron-right', 'share', 'book-open', 'copy'] as const;

/** Under the page: the browser's bottom bars of its kind, and the home indicator. */
export const PhoneBottom = ({ browser, host, onUnfold }: IBarsProps) => (
  <>
    {browser.id === 'ios26' && (
      <div className="hx-psafari" aria-hidden="true" onClick={onUnfold}>
        <span className="hx-psafari__btn">
          <Icon name="chevron-left" size="sm" />
        </span>
        <span className="hx-psafari__url">{host}</span>
        <span className="hx-psafari__btn">
          <Icon name="ellipsis" size="sm" />
        </span>
      </div>
    )}
    {browser.id === 'ios18' && (
      <div className="hx-ps18" aria-hidden="true" onClick={onUnfold}>
        <div className="hx-ps18__url">
          <span className="hx-ps18__aa">аА</span>
          <span className="hx-ps18__host">{host}</span>
          <Icon name="rotate-cw" size="sm" />
        </div>
        <div className="hx-ps18__tools">
          {TOOLS.map((name) => (
            <Icon key={name} name={name} size="sm" />
          ))}
        </div>
      </div>
    )}
    {browser.id === 'sam85' && (
      <div className="hx-psam" aria-hidden="true" onClick={onUnfold}>
        <span className="hx-psam__url">
          <Icon name="house" size="sm" />
          <span className="hx-psam__host">{host}</span>
        </span>
        <span className="hx-psam__side">
          <span className="hx-psam__tabs">2</span>
          <Icon name="menu" size="sm" />
        </span>
      </div>
    )}
    {browser.bot && (
      <div className="hx-pbot" aria-hidden="true">
        {TOOLS.map((name) => (
          <Icon key={name} name={name} size="sm" />
        ))}
      </div>
    )}
    <span className="hx-phome" aria-hidden="true" />
  </>
);

interface IOverviewProps {
  cards: readonly IOverviewCard[];
  /** The page at an address the slot lets in without a ticket; null until the slot has let the first frame in. */
  src: string | null;
  onPick: (id: string) => void;
}

/** The devices side by side, each with the page in its own size, scaled by one factor. */
export const Overview = ({ cards, src, onPick }: IOverviewProps) => (
  <div className="hx-ovw">
    {cards.map(({ device, w, h, scale }) => {
      const name = t(`next.site.devices.${device.id}`);
      return (
        <div
          key={device.id}
          className="hx-ovw__card"
          ref={(el) => {
            el?.style.setProperty('--w', `${w}px`);
            el?.style.setProperty('--h', `${h}px`);
            el?.style.setProperty('--s', scale.toFixed(4));
          }}
        >
          <button
            type="button"
            className="hx-ovw__label"
            aria-label={`${name}: ${t('next.site.overviewOpen')}`}
            onClick={() => onPick(device.id)}
          >
            <Icon name={device.icon} size="sm" />
            <span className="hx-ovw__name">{name}</span>
            <span className="hx-ovw__size">{`${w}×${h}`}</span>
          </button>
          <div className="hx-ovw__screen" data-round={w < 1000}>
            <div className="hx-ovw__scroll">
              {src !== null && <iframe className="hx-ovw__frame" src={src} title={name} />}
            </div>
          </div>
        </div>
      );
    })}
  </div>
);

interface IPhoneProps {
  /** The code's modules, rows of dark (true) and light ones. */
  modules: readonly (readonly boolean[])[];
  state: 'open' | 'closing';
  onCopy: () => void;
}

/** «Открыть на телефоне»: the code of the address of this page, to be pointed at with the phone's camera. */
export const PhonePopover = ({ modules, state, onCopy }: IPhoneProps) => (
  <div
    className="hx-site-menu hx-site-menu--end hx-phone"
    data-state={state}
    role="dialog"
    aria-label={t('next.site.phone')}
  >
    <div
      className="hx-qr"
      aria-hidden="true"
      ref={(el) => el?.style.setProperty('--n', String(modules.length))}
    >
      {modules.flatMap((row, y) => row.map((on, x) => <i key={`${y}:${x}`} data-on={on} />))}
    </div>
    <div className="hx-phone__title">{t('next.site.phoneTitle')}</div>
    <div className="hx-phone__sub">{t('next.site.phoneSub')}</div>
    <button type="button" className="hx-phone__send" onClick={onCopy}>
      <Icon name="link" size="sm" />
      {t('next.site.copyLink')}
    </button>
  </div>
);
