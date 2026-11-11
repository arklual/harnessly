// «Расширения»: the hub-settings views of the operator's extensions, each in a sandboxed frame. A view that
// fails to load or says nothing is drawn as a row with «Повторить»; it never takes the hub down.
import { Component, type ReactNode, useCallback, useEffect, useRef, useState } from 'react';

import { Titles } from '../shell';
import { Btn, Icon } from '../ui';
import { fromFrame, type HostView, useBranding, useHostUI } from '../ext/host';
import '../styles/extensions.css';

const READY_MS = 8000;

interface FrameProps {
  view: HostView;
  theme: string;
}

/** One view: the frame, the handshake and the failure row. */
export const ViewFrame = ({ view, theme }: FrameProps) => {
  const ref = useRef<HTMLIFrameElement>(null);
  const [state, setState] = useState<'wait' | 'ready' | 'failed'>('wait');
  const [tick, setTick] = useState(0);

  const retry = useCallback(() => {
    setState('wait');
    setTick((n) => n + 1);
  }, []);

  useEffect(() => {
    const on = (e: MessageEvent) => {
      const win = ref.current?.contentWindow;
      // The sandbox has no allow-same-origin, so a view's messages carry the origin "null".
      if (!fromFrame(e, win, 'null')) return;
      const d = e.data as { type?: unknown } | null;
      if (d?.type === 'harnessly.ext.hello') {
        win?.postMessage({ type: 'harnessly.ext.init', ext: view.ext, view: view.id, theme }, '*');
        setState('ready');
      } else if (d?.type === 'harnessly.ext.error') {
        setState('failed');
      }
    };
    window.addEventListener('message', on);
    const t = window.setTimeout(() => setState((s) => (s === 'wait' ? 'failed' : s)), READY_MS);
    return () => {
      window.removeEventListener('message', on);
      window.clearTimeout(t);
    };
  }, [view.ext, view.id, theme, tick]);

  return (
    <section className="hx-ext" aria-label={view.label}>
      <h3 className="hx-ext__h">{view.label}</h3>
      {state === 'failed' && (
        <div className="hx-ext__fail" role="alert">
          <Icon i="circle-alert" />
          <span>Расширение не отвечает.</span>
          <Btn size="sm" onClick={retry}>
            Повторить
          </Btn>
        </div>
      )}
      {state !== 'failed' && (
        <iframe
          key={tick}
          ref={ref}
          className="hx-ext__frame"
          title={view.label}
          src={view.url}
          sandbox="allow-scripts"
          referrerPolicy="no-referrer"
          loading="lazy"
          onError={() => setState('failed')}
        />
      )}
    </section>
  );
};

/** Whatever a view's rendering throws stays inside its own block. */
export class ViewBoundary extends Component<{ label: string; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <div className="hx-ext__fail" role="alert">
        <Icon i="circle-alert" />
        <span>{this.props.label}: расширение не удалось показать.</span>
        <Btn size="sm" onClick={() => this.setState({ failed: false })}>
          Повторить
        </Btn>
      </div>
    );
  }
}

const Extensions = () => {
  const ui = useHostUI();
  useBranding(ui?.branding);
  const theme = document.documentElement.dataset.hxTheme ?? 'light';
  return (
    <div className="hx-set">
      <Titles title="Расширения" lead="Настройки, которые добавили расширения этого хаба." />
      {ui === null && <div className="hxh-wait" role="status" aria-label="Загрузка" />}
      {ui !== null && ui.views.length === 0 && <p className="hx-ext__empty">Расширений с настройками нет.</p>}
      {ui?.views.map((v) => (
        <ViewBoundary key={`${v.ext}/${v.id}`} label={v.label}>
          <ViewFrame view={v} theme={theme} />
        </ViewBoundary>
      ))}
    </div>
  );
};

export default Extensions;
