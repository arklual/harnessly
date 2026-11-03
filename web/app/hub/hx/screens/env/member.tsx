// «Моё окружение»: what the person's chats get, with a personal switch on each line. A pinned line has a lock instead.
import { useCallback, useEffect, useState } from 'react';

import type { MeEnvResponse } from '../../../../hub-api/env';
import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { Titles } from '../../shell';
import { Icon } from '../../ui';
import { KIND_ICON, mineFiltered, type KindFilter, mineSource, withMineOff } from './helpers';
import { Empty, EnvSkeleton, isNotEnabled, LoadFailed } from './parts';
import { KindSegment } from './search';
import { Toast, useToast } from './toast';

export const MemberEnv = () => {
  const { api, scope } = useOrg();
  const space = scope.kind === 'tenant' ? scope.tenant.slug : undefined;
  const [data, setData] = useState<MeEnvResponse | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [tick, setTick] = useState(0);
  const [kind, setKind] = useState<KindFilter>('all');
  const toast = useToast();
  const [seen, setSeen] = useState(space);
  if (seen !== space) {
    setSeen(space);
    setData(null);
    setError(null);
  }

  useEffect(() => {
    let alive = true;
    api.envMine(space).then(
      (d) => alive && setData(d),
      (e: unknown) => alive && setError(e),
    );
    return () => {
      alive = false;
    };
  }, [api, space, tick]);

  const reload = useCallback(() => {
    setData(null);
    setError(null);
    setTick((n) => n + 1);
  }, []);

  const flip = async (itemId: string, off: boolean) => {
    // Shown at once; taken back, with the reason, when the hub refuses.
    setData((d) => (d ? { ...d, items: withMineOff(d.items, itemId, off) } : d));
    try {
      const answer = await api.envMineSet(itemId, off, space);
      setData((d) => (d ? { ...d, items: d.items.map((i) => (i.itemId === itemId ? answer : i)) } : d));
    } catch (e) {
      setData((d) => (d ? { ...d, items: withMineOff(d.items, itemId, !off) } : d));
      toast.show(orgErrorText(e));
    }
  };

  const notEnabled = isNotEnabled(error);
  const shown = data ? mineFiltered(data.items, kind) : [];

  return (
    <>
      <div className="ad-head">
        <Titles
          title="Моё окружение"
          lead="Сложено из организации, тенанта и команды. Лишнее можно выключить у себя, своё — добавить из каталога"
        />
      </div>
      {notEnabled ? (
        <Empty>Здесь пока ничего нет</Empty>
      ) : error != null ? (
        <LoadFailed error={error} onRetry={reload} />
      ) : !data ? (
        <EnvSkeleton tiles={false} />
      ) : (
        <div className="mo">
          <div className="mo-bar">
            <KindSegment value={kind} onChange={setKind} />
          </div>
          {data.items.length === 0 ? (
            <Empty>Здесь пока ничего нет</Empty>
          ) : shown.length === 0 ? (
            <Empty>Ничего не нашлось</Empty>
          ) : (
            <div className="hx-set__group">
              <div className="hx-set__card">
                {shown.map((i) => (
                  <div className="ad-row" key={i.itemId} data-off={i.off ? '1' : '0'}>
                    <span className="ad-ic">
                      <Icon i={KIND_ICON[i.kind]} size="sm" />
                    </span>
                    <span className="ad-row__t">
                      <b>{i.name}</b>
                      <small>{i.off ? 'Выключено у меня' : i.hint}</small>
                    </span>
                    <span className="ad-src" data-l={i.layer}>
                      {mineSource(i)}
                    </span>
                    {i.canToggle ? (
                      <button
                        type="button"
                        className="hx-switch"
                        role="switch"
                        aria-checked={i.off ? 'false' : 'true'}
                        aria-label={`${i.off ? 'Включить' : 'Выключить'} «${i.name}» у себя`}
                        onClick={() => void flip(i.itemId, !i.off)}
                      >
                        <i />
                      </button>
                    ) : (
                      <span
                        className="en-lock"
                        title="Закреплено — здесь не выключить"
                        role="img"
                        aria-label="Закреплено — здесь не выключить"
                      >
                        <Icon i="lock" size="sm" />
                      </span>
                    )}
                  </div>
                ))}
              </div>
              <div className="hx-set__foot">
                Выключенное здесь пропадёт только у тебя. Команда и тенант ничего не заметят.
              </div>
            </div>
          )}
        </div>
      )}
      <Toast text={toast.text} id={toast.n} onClose={toast.hide} />
    </>
  );
};
