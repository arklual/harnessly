// /org/new on the new hub's kit: a name, the spaces to move in and, with none moved, the first tenant's name.
// Pessimistic like the old step: the button spins until the hub answers.
import { useEffect, useMemo, useState } from 'react';

import { navigate } from '../../lib/router';
import { createOrgApi } from '../../org/api';
import { orgErrorText } from '../../org/api/errors';
import type { LooseSpace } from '../../org/api/onboarding';
import { dataMode, mockRole } from '../../org/mode';
import { deferOrgStep } from '../../org/step';
import { Btn, Icon } from '../ui';
import { AuthFrame, Note } from './frame';

const NAME_MAX = 60;

export const HxOrgNew = () => {
  const api = useMemo(() => createOrgApi(dataMode(), mockRole()), []);
  const [spaces, setSpaces] = useState<LooseSpace[] | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [name, setName] = useState('');
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set());
  const [tenantName, setTenantName] = useState('');
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
  const tenantClean = tenantName.trim();
  const tooLong = `Не длиннее ${NAME_MAX} знаков`;
  const nameError = clean.length > NAME_MAX ? tooLong : null;
  const tenantError = tenantClean.length > NAME_MAX ? tooLong : null;
  const needsTenant = spaces !== null && picked.size === 0;
  const canCreate = clean !== '' && nameError === null && (!needsTenant || tenantError === null) && !busy;

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
      const { slug } = await api.createOrg({
        name: clean,
        spaceIds: [...picked],
        ...(picked.size === 0 ? { tenantName: tenantClean } : {}),
      });
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
    <AuthFrame
      title="Организация"
      lead="Соберите пространства в организацию: общая картина, команды и лимиты. Машины и агенты продолжат работать как раньше."
    >
      <form
        className="hxa__stack"
        onSubmit={(e) => {
          e.preventDefault();
          if (canCreate) void create();
        }}
      >
        <label className="hxa__field">
          <span className="hxa__label">Название организации</span>
          <input
            className="hx-set__input"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Например, Акме"
            aria-invalid={nameError ? true : undefined}
            autoFocus
          />
          {nameError && <span className="hxa__err">{nameError}</span>}
        </label>

        <fieldset className="hxa__checks">
          <legend className="hxa__label">Какие пространства перенести</legend>
          {loadError != null && <Note tone="danger">{orgErrorText(loadError)}</Note>}
          {spaces === null && loadError == null && (
            <p className="hxa__note" role="status">
              <Icon i="loader-circle" /> Загружаем пространства
            </p>
          )}
          {spaces?.length === 0 && (
            <p className="hxa__note">Своих пространств пока нет — организацию можно собрать позже.</p>
          )}
          {spaces?.map((s) => (
            <label key={s.id} className="hxa__check" data-off={!s.owner || undefined}>
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

        {needsTenant && (
          <label className="hxa__field">
            <span className="hxa__label">Первый тенант</span>
            <input
              className="hx-set__input"
              value={tenantName}
              onChange={(e) => setTenantName(e.target.value)}
              placeholder="Основное"
              aria-invalid={tenantError ? true : undefined}
            />
            <span className="hxa__note">
              Тенант — рабочее пространство: в нём команды и машины. Название можно изменить позже.
            </span>
            {tenantError && <span className="hxa__err">{tenantError}</span>}
          </label>
        )}

        {error && <Note tone="danger">{error}</Note>}
        <div className="hxa__actions">
          <Btn type="submit" variant="primary" size="lg" busy={busy} disabled={!canCreate}>
            Создать организацию
          </Btn>
          <Btn type="button" variant="ghost" size="lg" onClick={later} disabled={busy}>
            Позже
          </Btn>
        </div>
      </form>
    </AuthFrame>
  );
};
