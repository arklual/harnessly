import {
  BookOpen,
  ChevronLeft,
  ChevronRight,
  Copy,
  EllipsisVertical,
  Lock,
  RotateCw,
  Share,
  SlidersHorizontal,
} from 'lucide-react';
import { cx } from '@/components/agents/ui';
import {
  DEVICE_FRAME,
  DEVICE_PLATFORM,
  PREVIEW_BROWSER,
  type IDeviceSpec,
  type TPreviewBrowserId,
} from '@/lib/hl-preview-devices';

const NOTCH_FLARE = 8;
const NOTCH_HEIGHT = 34;
const NOTCH_RADIUS = 20;

/** The notch as one shape: concave flares at the screen edge, straight sides, round bottom corners. */
const notchPath = (width: number): string => {
  const w = width + NOTCH_FLARE * 2;
  const f = NOTCH_FLARE;
  const h = NOTCH_HEIGHT;
  const r = NOTCH_RADIUS;
  return `M0 0Q${f} 0 ${f} ${f}V${h - r}Q${f} ${h} ${f + r} ${h}H${w - f - r}Q${w - f} ${h} ${w - f} ${h - r}V${f}Q${w - f} 0 ${w} 0Z`;
};

const SignalIcon = () => (
  <svg viewBox="0 0 18 12" width="18" height="12" fill="currentColor" aria-hidden>
    <rect x="0" y="8" width="3" height="4" rx="0.8" />
    <rect x="5" y="5.5" width="3" height="6.5" rx="0.8" />
    <rect x="10" y="3" width="3" height="9" rx="0.8" />
    <rect x="15" y="0" width="3" height="12" rx="0.8" />
  </svg>
);

const WifiIcon = () => (
  <svg
    viewBox="0 0 17 12"
    width="17"
    height="12"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    aria-hidden
  >
    <path d="M1.5 4.2a10 10 0 0 1 14 0" />
    <path d="M4.2 7a6.2 6.2 0 0 1 8.6 0" />
    <circle cx="8.5" cy="10.2" r="1.1" fill="currentColor" stroke="none" />
  </svg>
);

const BatteryIcon = () => (
  <svg viewBox="0 0 27 13" width="27" height="13" fill="none" aria-hidden>
    <rect x="0.5" y="0.5" width="22" height="12" rx="3.6" stroke="currentColor" opacity="0.4" />
    <rect x="2" y="2" width="19" height="9" rx="2.4" fill="currentColor" />
    <path d="M24.2 4.4v4.2c.9-.3 1.6-1.2 1.6-2.1s-.7-1.8-1.6-2.1Z" fill="currentColor" opacity="0.5" />
  </svg>
);

interface IStatusBarProps {
  device: IDeviceSpec;
}

const StatusBar = ({ device }: IStatusBarProps) => {
  const android = device.platform === DEVICE_PLATFORM.android;
  return (
    <div className="tw-device-status">
      <span className="tw-device-status-time">{android ? '12:30' : '9:41'}</span>
      <span className="tw-device-status-gap" />
      <span className="tw-device-status-icons">
        {android ? (
          <>
            <WifiIcon />
            <SignalIcon />
          </>
        ) : (
          <>
            <SignalIcon />
            <WifiIcon />
          </>
        )}
        <BatteryIcon />
      </span>
    </div>
  );
};

interface IAddressProps {
  host: string;
}

const SafariBar = ({ host }: IAddressProps) => (
  <div className="tw-device-safari-bar">
    <div className="tw-device-safari-pill">
      <span className="tw-device-safari-aa">aA</span>
      <Lock />
      <span className="tw-device-address-host">{host}</span>
      <RotateCw />
    </div>
    <div className="tw-device-safari-toolbar">
      <ChevronLeft />
      <ChevronRight />
      <Share />
      <BookOpen />
      <Copy />
    </div>
  </div>
);

const ChromeBar = ({ host }: IAddressProps) => (
  <div className="tw-device-chrome-bar">
    <div className="tw-device-chrome-pill">
      <SlidersHorizontal />
      <span className="tw-device-address-host">{host}</span>
    </div>
    <span className="tw-device-chrome-tabs">1</span>
    <EllipsisVertical />
  </div>
);

interface IDeviceChromeProps {
  device: IDeviceSpec;
  browser: TPreviewBrowserId;
  host: string;
}

/**
 * Everything the device and its browser draw over the page: status bar, cutout, address bars, home indicator. It takes
 * no pointer events, the page below stays usable. Without browser chrome the page runs under the bars, and their
 * glyphs invert against it (see `tw-device-bare`).
 */
export const DeviceChrome = ({ device, browser, host }: IDeviceChromeProps) => {
  const bare = browser === PREVIEW_BROWSER.none;
  return (
    <div
      className={cx(
        'tw-device-chrome',
        `tw-device-${device.platform}`,
        `tw-device-frame-${device.frame}`,
        `tw-device-browser-${browser}`,
        bare && 'tw-device-bare',
      )}
      aria-hidden
    >
      <div className="tw-device-top">
        <StatusBar device={device} />
        {browser === PREVIEW_BROWSER.chrome && <ChromeBar host={host} />}
      </div>
      {browser === PREVIEW_BROWSER.safari && <SafariBar host={host} />}
      {device.frame === DEVICE_FRAME.notch && (
        <svg
          className="tw-device-notch"
          width={device.cutout + NOTCH_FLARE * 2}
          height={NOTCH_HEIGHT}
          viewBox={`0 0 ${device.cutout + NOTCH_FLARE * 2} ${NOTCH_HEIGHT}`}
        >
          <path d={notchPath(device.cutout)} fill="#000" />
        </svg>
      )}
      {device.frame === DEVICE_FRAME.island && <span className="tw-device-island" />}
      {device.frame === DEVICE_FRAME.punch && <span className="tw-device-punch" />}
      {device.homeInset > 0 && <span className="tw-device-home" />}
    </div>
  );
};
