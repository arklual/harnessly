// «{design}» in Settings: which engine new designs get on this machine and which tools it has. Designs work on the
// built-in engine with any account; the vendor's engine is the person's choice, added with one command in a terminal.
// The page reads the machine again when the person comes back to it, so running the command needs no reload.
import { useEffect, useState } from 'react';
import { fetchEngine, type IDesignEngine } from '@/next/features/designly/api';
import CopyCommand from '@/next/features/settings/copy-command';
import { Group, Note, Row } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { Skeleton } from '@/next/ui/skeleton';
import Swap from '@/next/ui/swap';

type TEngineRead = { machine: MachineId; value: IDesignEngine | 'failed' };

const useEngine = (machine: MachineId): IDesignEngine | 'failed' | null => {
  const [read, setRead] = useState<TEngineRead | null>(null);
  useEffect(() => {
    const abort = new AbortController();
    const load = () => {
      fetchEngine(machine, abort.signal).then(
        (value) => setRead({ machine, value }),
        () => {
          if (!abort.signal.aborted) setRead({ machine, value: 'failed' });
        },
      );
    };
    load();
    const onFocus = () => {
      if (!document.hidden) load();
    };
    window.addEventListener('focus', onFocus);
    document.addEventListener('visibilitychange', onFocus);
    return () => {
      abort.abort();
      window.removeEventListener('focus', onFocus);
      document.removeEventListener('visibilitychange', onFocus);
    };
  }, [machine]);
  return read?.machine === machine ? read.value : null;
};

const ENGINE_LABEL = {
  own: 'next.settings.design.own',
  vendor: 'next.settings.design.vendor',
  file: 'next.settings.design.file',
} as const satisfies Record<IDesignEngine['source'], string>;

const engineHint = (engine: IDesignEngine): string | undefined => {
  if (engine.source === 'own') return t('next.settings.design.ownHint');
  if (engine.source === 'vendor' && engine.release)
    return t('next.settings.design.vendorHint', { release: engine.release });
  return undefined;
};

const toolState = (on: boolean) => t(on ? 'next.settings.design.installed' : 'next.settings.design.missing');

export const DesignPage = ({ machine }: { machine: MachineId }) => {
  const engine = useEngine(machine);
  if (engine === 'failed') return <Note>{t('next.settings.design.unsupported')}</Note>;
  return (
    <Swap ready={engine !== null} skeleton={<Skeleton className="hx-set__sk" />}>
      {engine && (
        <>
          <Group title={t('next.settings.design.engine')}>
            <Row type="link" label={t(ENGINE_LABEL[engine.source])} hint={engineHint(engine)}>
              {null}
            </Row>
            {engine.source !== 'file' && engine.fetch && (
              <>
                <Row
                  type="link"
                  label={t(
                    engine.source === 'vendor'
                      ? 'next.settings.design.upstreamUpdate'
                      : 'next.settings.design.upstream',
                  )}
                  hint={t('next.settings.design.upstreamHint')}
                >
                  {null}
                </Row>
                <div className="hx-set__row">
                  <CopyCommand command={engine.fetch} />
                </div>
              </>
            )}
          </Group>
          <Group title={t('next.settings.design.tools')}>
            <Row
              type="link"
              label={t('next.settings.design.chrome')}
              hint={t('next.settings.design.chromeHint')}
            >
              <span className="hx-set__row-hint">{toolState(engine.chrome)}</span>
            </Row>
            <Row
              type="link"
              label={t('next.settings.design.ffmpeg')}
              hint={t('next.settings.design.ffmpegHint')}
            >
              <span className="hx-set__row-hint">{toolState(engine.ffmpeg)}</span>
            </Row>
          </Group>
        </>
      )}
    </Swap>
  );
};
