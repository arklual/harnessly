// «Организация»: a name and the spaces to move in. Pessimistic: the button spins until the hub answers, and only then
// the hub opens on the new organization's Обзор. «Позже» keeps everything as it was.
import './org-step.css';

import { useState } from 'react';

import { Button, Spinner, TextField } from '@kit';

import { navigate } from '../../lib/router';
import { PlainPage } from '../../ui/frame';
import { createOrgApi } from '../api';
import { orgErrorText } from '../api/errors';
import { dataMode, mockRole } from '../mode';
import { deferOrgStep } from '../step';
import { useEffect, useMemo } from 'react';
import type { LooseSpace } from '../api/onboarding';

const NAME_MAX = 60;

export const OrgStepScreen = () => {
  const api = useMemo(() => createOrgApi(dataMode(), mockRole()), []);
  const [spaces, setSpaces] = useState<LooseSpace[] | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [name, setName] = useState('');
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api.looseSpaces().then(
      (list) => {
        if (!alive) return;
        setSpaces(list);
        setPicked(new Set(list.filter((s) => s.owner).map((s) => s.id)));
      },
      (e: unknown) => alive && setLoadError(e),
    );
    return () => {
      alive = false;
    };
  }, [api]);

  const clean = name.trim();
  const nameError = clean.length > NAME_MAX ? `Не длиннее ${NAME_MAX} знаков` : null;
  const canCreate = clean !== '' && nameError === null && !busy;

  const toggle = (id: string) =>
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const create = async () => {
    setBusy(true);
    setError(null);
    try {
      const { slug } = await api.createOrg({ name: clean, spaceIds: [...picked] });
      navigate(`/o/${slug}/all/overview`);
    } catch (e) {
      setError(orgErrorText(e));
      setBusy(false);
    }
  };

  const later = () => {
    deferOrgStep();
    navigate('/', { replace: true });
  };

  return (
    <PlainPage title="Организация">
      <div className="ho-step">
        <p className="ho-step__lead">
          Соберите пространства в организацию: общая картина, команды и лимиты. Машины и агенты продолжат
          работать как раньше.
        </p>
        <TextField
          label="Название организации"
          value={name}
          onChange={(e) => setName(e.target.value)}
          error={nameError ?? undefined}
          placeholder="Например, Акме"
          maxLength={NAME_MAX + 20}
          autoFocus
        />
        <fieldset className="ho-step__spaces">
          <legend>Какие пространства перенести</legend>
          {loadError != null && <p className="ho-step__err">{orgErrorText(loadError)}</p>}
          {spaces === null && loadError == null && (
            <p className="ho-step__wait" role="status">
              <Spinner size="s" /> Загружаем пространства
            </p>
          )}
          {spaces?.length === 0 && (
            <p className="ho-step__wait">Своих пространств пока нет — организацию можно собрать позже.</p>
          )}
          {spaces?.map((s) => (
            <label key={s.id} className="ho-step__space" data-off={!s.owner || undefined}>
              <input
                type="checkbox"
                checked={picked.has(s.id)}
                disabled={!s.owner || busy}
                onChange={() => toggle(s.id)}
              />
              <span>{s.name}</span>
              {!s.owner && <small>переносит владелец</small>}
            </label>
          ))}
        </fieldset>
        {error && (
          <p className="ho-step__err" role="alert">
            {error}
          </p>
        )}
        <div className="ho-step__actions">
          <Button onClick={() => void create()} disabled={!canCreate}>
            {busy && <Spinner size="xs" />} Создать организацию
          </Button>
          <Button appearance="ghost" onClick={later} disabled={busy}>
            Позже
          </Button>
        </div>
      </div>
    </PlainPage>
  );
};
