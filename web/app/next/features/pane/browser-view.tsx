// A browser tab of the pane (.hx-tbrowser): back, forward, reload, the address; with no address yet, the servers that
// listen on the machine, the chat's own first. The page is the address opened in a preview slot.
import { useState } from 'react';
import Icon from '@/next/ui/icon';
import { parseAddress, type IAddress } from '@/next/features/pane/address';
import type { IListeningPort } from '@/next/core/net/preview';
import { t } from '@/next/i18n';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';

export interface IBrowserViewProps {
  /** What the bar shows; empty for a new tab. */
  address: string;
  canBack: boolean;
  canForward: boolean;
  /** The slot's entry for the page; null while it is asked for. */
  src: string | null;
  error: string | null;
  /** The machine's servers for a new tab; null while they load. */
  ports: IListeningPort[] | null;
  portsError: string | null;
  /** Asks for the servers again. */
  onRetryPorts: () => void;
  reloadKey: number;
  onGo: (address: IAddress) => void;
  onHistory: (step: -1 | 1) => void;
  onReload: () => void;
  onNewTab: () => void;
}

const BrowserView = (props: IBrowserViewProps) => {
  const { address, canBack, canForward, src, error, ports, portsError, onRetryPorts, reloadKey } = props;
  const [draft, setDraft] = useState<string | null>(null);
  const typed = draft ?? address;

  return (
    <div className="hx-tv" data-tool="browser">
      <div className="hx-tbrowser">
        <div className="hx-tbrowser__bar">
          <button
            type="button"
            className="hx-head-btn"
            data-on={canBack}
            aria-label={t('next.site.back')}
            onClick={() => props.onHistory(-1)}
          >
            <Icon name="arrow-left" size="sm" />
          </button>
          <button
            type="button"
            className="hx-head-btn"
            data-on={canForward}
            aria-label={t('next.site.forward')}
            onClick={() => props.onHistory(1)}
          >
            <Icon name="arrow-right" size="sm" />
          </button>
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.site.reload')}
            onClick={props.onReload}
          >
            <Icon name="rotate-cw" size="sm" />
          </button>
          <input
            className="hx-tbrowser__url"
            type="text"
            autoComplete="off"
            spellCheck={false}
            placeholder={t('next.pane.address')}
            aria-label={t('next.pane.addressLabel')}
            aria-invalid={draft !== null && draft.trim() !== '' && !parseAddress(draft)}
            value={typed}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={() => setDraft(null)}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                setDraft(null);
                return;
              }
              if (event.key !== 'Enter') return;
              const next = parseAddress(typed);
              if (!next) return;
              setDraft(null);
              props.onGo(next);
            }}
          />
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={t('next.pane.newBrowserTab')}
            onClick={props.onNewTab}
          >
            <Icon name="plus" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.pane.newBrowserTab')}
            </span>
          </button>
        </div>
        {address === '' && (
          <>
            <div className="hx-tv__label">{t('next.pane.servers')}</div>
            {ports === null && portsError === null && (
              <div className="hx-tbrowser__note">{t('next.pane.serversLoading')}</div>
            )}
            {portsError !== null && (
              <LoadFailed
                size={LOAD_FAILED_SIZE.inline}
                title={t('next.load.ports')}
                reason={portsError}
                onRetry={onRetryPorts}
              />
            )}
            {ports?.length === 0 && <div className="hx-tbrowser__note">{t('next.pane.noServers')}</div>}
            {ports && ports.length > 0 && (
              <div className="hx-srv">
                {ports.map((p, i) => {
                  const next = parseAddress(String(p.port));
                  const name = p.title ?? p.command ?? `:${p.port}`;
                  const where = [`localhost:${p.port}`, p.title ? p.command : null]
                    .filter(Boolean)
                    .join(' · ');
                  return (
                    <button
                      key={p.port}
                      type="button"
                      className="hx-srv__card"
                      data-i={Math.min(i, 8)}
                      onClick={() => next && props.onGo(next)}
                    >
                      <span className="hx-srv__ico">
                        {p.icon ? (
                          <img src={p.icon} alt="" width={16} height={16} />
                        ) : (
                          <Icon name="globe" size="sm" />
                        )}
                      </span>
                      <span className="hx-srv__main">
                        <span className="hx-srv__name">{name}</span>
                        {p.description && <span className="hx-srv__about">{p.description}</span>}
                        <span className="hx-srv__where">
                          {where}
                          {p.group === 'chat' && <b>{t('next.pane.thisChat')}</b>}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </>
        )}
        {address !== '' && error !== null && (
          <LoadFailed title={t('next.load.page')} reason={error} onRetry={props.onReload} />
        )}
        {address !== '' && error === null && src === null && <div className="hx-tbrowser__page" />}
        {address !== '' && src !== null && (
          <iframe
            key={reloadKey}
            className="hx-tbrowser__frame"
            src={src}
            title={address}
            allow="clipboard-write"
          />
        )}
      </div>
    </div>
  );
};

export default BrowserView;
