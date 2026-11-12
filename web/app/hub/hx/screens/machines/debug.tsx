// Debug mode of a machine (docs/hub/own-work.md, «Debug mode»): under the owner's rule a role manages a machine without
// reading its chats; the panel opens it for 30 minutes with a reason, and the card shows its owner who opened it and why.
import { useEffect, useState } from 'react';

import type { DebugOpening } from '../../../org/api/machines';
import { useOrg } from '../../../org/context';
import { Btn, Icon } from '../../ui';

const MIN = 10;
const MAX = 500;

/** The way in for a person whose role the owner's rule keeps out. */
export const DebugPanel = ({
  busy,
  failed,
  onOpen,
  onClose,
}: {
  busy: boolean;
  /** Why the last try to open for debugging failed, as the hub said. */
  failed: string | null;
  onOpen: (reason: string) => void;
  onClose: () => void;
}) => {
  const [reason, setReason] = useState('');
  const n = reason.trim().length;
  const ok = n >= MIN && n <= MAX;
  return (
    <div className="hxa__msg hxa__trust" data-tone="warn" role="alert">
      <Icon i="shield-check" />
      <form
        className="hxa__stack"
        onSubmit={(e) => {
          e.preventDefault();
          if (ok) onOpen(reason.trim());
        }}
      >
        <strong>Это чужая работа</strong>
        <span>
          Ваша роль позволяет управлять машиной, но не читать чаты её владельца. Если нужно разобраться, что
          на ней происходит, откройте её для отладки на 30 минут. Владелец получит письмо с причиной и увидит
          открытие в карточке машины.
        </span>
        <textarea
          className="hx-set__input ev-ta ev-ta--sm"
          rows={2}
          maxLength={MAX}
          placeholder="Причина: что случилось и что вы будете смотреть"
          aria-label="Причина отладки"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        {failed && <span className="md-err">{failed}</span>}
        <div className="hxa__actions">
          <Btn variant="primary" size="sm" type="submit" busy={busy} disabled={!ok}>
            Открыть для отладки на 30 минут
          </Btn>
          <Btn variant="ghost" size="sm" onClick={onClose}>
            Закрыть
          </Btn>
        </div>
      </form>
    </div>
  );
};

const when = (iso: string) =>
  new Date(iso).toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

/** Who opened the machine for debugging and why. Shown to its owner and to those who manage it; nothing when empty. */
export const DebugLog = ({ id }: { id: string }) => {
  const { api } = useOrg();
  const [rows, setRows] = useState<DebugOpening[] | null>(null);
  useEffect(() => {
    let live = true;
    api.debugOpenings(id).then(
      (r) => live && setRows(r),
      () => live && setRows(null),
    );
    return () => {
      live = false;
    };
  }, [api, id]);
  if (!rows || rows.length === 0) return null;
  return (
    <div className="md-sec">
      <div className="md-sec__t">Открывали для отладки</div>
      <ul className="md-debug">
        {rows.map((o) => (
          <li key={o.id}>
            <b>{o.name || o.email}</b> · {when(o.at)}–{when(o.until).split(', ').pop()}
            <p>{o.reason}</p>
          </li>
        ))}
      </ul>
    </div>
  );
};
