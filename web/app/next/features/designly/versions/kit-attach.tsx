// «Подключить UI-кит» over a project that has none: an installed kit in one click, or a package (npm with
// an optional registry) that is made into a kit, drawn once, installed and written into the project. From then on the
// project's new boards start with the kit's files and an agent draws only from it.
import { useState } from 'react';
import Popover from '@/next/ui/popover';
import { Button } from '@/next/ui/button';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  attachProjectKit,
  connectProjectKit,
  type IProjectKit,
} from '@/next/features/designly/versions/versions-api';
import '@/next/styles/versions.css';

export interface IKitAttachProps {
  machine: MachineId;
  id: string;
  installed: string[];
  onDone: (kit: IProjectKit) => void;
}

/** Whether a project offers «Подключить UI-кит»: it is not on a kit and is not a kit's draft. */
export const canAttachKit = (kit: IProjectKit | null): kit is IProjectKit =>
  kit !== null && kit.pin === null && kit.kitDraftOf === undefined;

const KitAttach = ({ machine, id, installed, onDone }: IKitAttachProps) => {
  const [open, setOpen] = useState(false);
  const [source, setSource] = useState('');
  const [registry, setRegistry] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const run = async (key: string, call: () => Promise<IProjectKit>) => {
    setBusy(key);
    setError(null);
    try {
      const next = await call();
      setOpen(false);
      onDone(next);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('next.versions.failed'));
    } finally {
      setBusy(null);
    }
  };
  const named = source.trim();
  return (
    <div className="hx-kitband" data-tone="quiet">
      <span className="hx-kitband__actions">
        <Popover
          open={open}
          onOpenChange={setOpen}
          label={t('next.versions.attach')}
          className="hx-kitpop"
          side="bottom"
          align="end"
          trigger={
            <Button size="sm" variant="ghost" icon="plus">
              {t('next.versions.attach')}
            </Button>
          }
        >
          <div className="hx-kitpop__body">
            <div className="hx-kitpop__title">{t('next.versions.attachTitle')}</div>
            {installed.map((kit) => (
              <Button
                key={kit}
                size="sm"
                variant="ghost"
                busy={busy === kit}
                onClick={() => void run(kit, () => attachProjectKit(machine, id, kit))}
              >
                {kit}
              </Button>
            ))}
            <input
              className="hx-kitpop__field"
              value={source}
              placeholder={t('next.versions.attachPackage')}
              onChange={(e) => setSource(e.target.value)}
            />
            {named !== '' && (
              <input
                className="hx-kitpop__field"
                value={registry}
                placeholder={t('next.versions.attachRegistry')}
                onChange={(e) => setRegistry(e.target.value)}
              />
            )}
            {error !== null && <div className="hx-kitpop__error">{error}</div>}
            {named !== '' && (
              <div className="hx-kitpop__actions">
                <Button
                  size="sm"
                  variant="primary"
                  busy={busy === 'package'}
                  onClick={() =>
                    void run('package', () => connectProjectKit(machine, id, named, registry.trim()))
                  }
                >
                  {t('next.versions.attachConnect')}
                </Button>
              </div>
            )}
          </div>
        </Popover>
      </span>
    </div>
  );
};

export default KitAttach;
