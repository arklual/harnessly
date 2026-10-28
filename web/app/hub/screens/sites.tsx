// «Сайты» of a space: the list (a section of the home page), «Новый сайт» with a zip or a folder, and a site's page —
// where it opens, its versions with the way back to an older one, who may open it, the keys for CI, and the end of it.
import { useCallback, useEffect, useRef, useState, type DragEvent } from 'react';

import { Button, Dialog, EmptyState, TextField } from '@kit';

import { isApiError } from '../lib/api';
import { useApi, useEnv } from '../lib/env';
import { t } from '../lib/i18n';
import { Link, navigate } from '../lib/router';
import { useSession } from '../lib/session';
import {
  bareAddress,
  deployFiles,
  fromDrop,
  fromInput,
  isZip,
  siteErrorText,
  SITE_NAME,
  slugName,
  trimRoot,
  whenText,
  type PickedFile,
} from '../lib/sites';
import type { Site, SiteAccess, SiteToken } from '../lib/types';
import { formatBytes } from '../lib/format';
import { PlainPage } from '../ui/frame';
import { ExternalLink, Folder, Plus, Trash2 } from '../ui/icons';
import { CopyBox, Label, ListSkeleton, Msg, Note, Segmented } from '../ui/parts';
import { SpaceMenu } from './home';
import { useSpace } from './space';

/** Whether this hub serves sites at all: no domain in the cloud, the switch off. */
export const useSitesOn = (): boolean => useSession().config.sites?.enabled === true;

const useSites = (): { sites: Site[] | null; error: string | null; reload: () => void } => {
  const api = useApi();
  const { space } = useSpace();
  const [sites, setSites] = useState<Site[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let alive = true;
    api.sites.list(space.slug).then(
      (r) => {
        if (!alive) return;
        setSites(r.sites);
        setError(null);
      },
      (e: unknown) => {
        if (alive) setError(siteErrorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api, space.slug, tick]);
  return { sites, error, reload: () => setTick((n) => n + 1) };
};

export const SitesSection = () => {
  const { space } = useSpace();
  const { sites, error, reload } = useSites();

  const render = () => {
    if (error)
      return (
        <Msg
          tone="danger"
          action={
            <button type="button" className="hub-link" onClick={reload}>
              {t('common.retry')}
            </button>
          }
        >
          {error}
        </Msg>
      );
    if (sites === null) return <ListSkeleton rows={2} />;
    if (sites.length === 0)
      return (
        <EmptyState
          icon={<Folder size={28} />}
          title={t('sites.empty.title')}
          actions={
            <Button appearance="primary" onClick={() => navigate(`/s/${space.slug}/sites/new`)}>
              {t('sites.new')}
            </Button>
          }
        >
          {t('sites.empty.text')}
        </EmptyState>
      );
    return (
      <div className="hub-list">
        {sites.map((s) => (
          <Link key={s.id} to={`/s/${space.slug}/sites/${s.name}`} className="hub-item hub-site">
            <span className="hub-site__n">{s.name}</span>
            <span className="hub-site__a">{bareAddress(s.address)}</span>
            <span className="hub-item__s">
              {t('sites.updated', { when: whenText(s.updatedAt), who: s.updatedBy })}
            </span>
          </Link>
        ))}
      </div>
    );
  };

  return (
    <section className="hub-section">
      <div className="hub-section__h">
        <h2 className="hub-section__t">{t('sites.title')}</h2>
        {sites !== null && sites.length > 0 && (
          <Button size="s" icon={<Plus size={14} />} onClick={() => navigate(`/s/${space.slug}/sites/new`)}>
            {t('sites.new')}
          </Button>
        )}
      </div>
      {render()}
    </section>
  );
};

type Source = { zip: File } | { files: PickedFile[] };

const ACCESS_OPTIONS = (): { value: SiteAccess; label: string }[] => [
  { value: 'team', label: t('sites.access.team') },
  { value: 'link', label: t('sites.access.link') },
];

const sourceLine = (s: Source): string =>
  'zip' in s
    ? t('sites.pick.zip', { name: s.zip.name, size: formatBytes(s.zip.size) })
    : t('sites.pick.folder', {
        n: s.files.length,
        size: formatBytes(s.files.reduce((sum, f) => sum + f.file.size, 0)),
      });

const Dropzone = ({ source, onPick }: { source: Source | null; onPick: (s: Source) => void }) => {
  const [over, setOver] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const folder = useRef<HTMLInputElement | null>(null);
  const zip = useRef<HTMLInputElement | null>(null);

  const setFolderInput = useCallback((el: HTMLInputElement | null) => {
    folder.current = el;
    el?.setAttribute('webkitdirectory', '');
  }, []);

  const drop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    void fromDrop(e.dataTransfer).then((d) => {
      if (d === null) setProblem(t('sites.pick.nothing'));
      else {
        setProblem(null);
        onPick('zip' in d ? d : { files: trimRoot(d.files) });
      }
    });
  };

  return (
    <div className="hub-stack">
      <div
        className="hub-drop"
        data-over={over || undefined}
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={drop}
      >
        <Folder size={28} />
        <strong>{source ? sourceLine(source) : t('sites.drop.title')}</strong>
        <span className="hub-item__s">{t('sites.drop.hint')}</span>
        <div className="hub-row">
          <Button size="s" onClick={() => folder.current?.click()}>
            {t('sites.drop.folder')}
          </Button>
          <Button size="s" onClick={() => zip.current?.click()}>
            {t('sites.drop.zip')}
          </Button>
        </div>
        <input
          ref={setFolderInput}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            const list = e.target.files;
            if (list && list.length > 0) {
              setProblem(null);
              onPick({ files: fromInput(list) });
            }
            e.target.value = '';
          }}
        />
        <input
          ref={zip}
          type="file"
          accept=".zip,application/zip"
          hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f && isZip(f)) {
              setProblem(null);
              onPick({ zip: f });
            } else if (f) setProblem(t('sites.pick.notZip'));
            e.target.value = '';
          }}
        />
      </div>
      {problem && <Msg tone="danger">{problem}</Msg>}
    </div>
  );
};

/** Sends a source to a site by the way it is: a zip in one request, a folder by the protocol. */
const useSend = () => {
  const api = useApi();
  const { space } = useSpace();
  const [progress, setProgress] = useState<{ sent: number; total: number } | null>(null);
  const send = async (name: string, source: Source) => {
    setProgress({ sent: 0, total: 0 });
    try {
      if ('zip' in source) return await api.sites.upload(space.slug, name, source.zip);
      return await deployFiles(api, space.slug, name, source.files, (sent, total) =>
        setProgress({ sent, total }),
      );
    } finally {
      setProgress(null);
    }
  };
  return { send, progress };
};

export const NewSiteScreen = () => {
  const api = useApi();
  const { space } = useSpace();
  const { send, progress } = useSend();
  const [name, setName] = useState('');
  const [touched, setTouched] = useState(false);
  const [access, setAccess] = useState<SiteAccess>('team');
  const [source, setSource] = useState<Source | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const pick = (s: Source) => {
    setSource(s);
    // The name follows the zip's or the folder's until the person types their own.
    if (!touched && name === '') setName('zip' in s ? slugName(s.zip.name) : '');
  };

  const clean = name.trim();
  const nameOk = SITE_NAME.test(clean) && !clean.includes('--');

  const publish = async () => {
    if (!source || !nameOk) return;
    setBusy(true);
    setError(null);
    let created = false;
    try {
      await api.sites.create(space.slug, { name: clean, access });
      created = true;
      await send(clean, source);
      navigate(`/s/${space.slug}/sites/${clean}`);
    } catch (e) {
      // A site made for a deploy that failed is taken back: the person tries again with the same name.
      if (created) await api.sites.remove(space.slug, clean).catch(() => undefined);
      setError(siteErrorText(e));
      setBusy(false);
    }
  };

  return (
    <PlainPage title={t('sites.newTitle')} side={<SpaceMenu current={space.slug} />}>
      <p className="hub-note">
        <Link to={`/s/${space.slug}`} className="hub-link">
          {t('sites.back', { name: space.name })}
        </Link>
      </p>
      <Dropzone source={source} onPick={pick} />
      <TextField
        size="l"
        label={<Label required>{t('sites.name')}</Label>}
        autoComplete="off"
        placeholder={t('sites.namePlaceholder')}
        value={name}
        error={name !== '' && !nameOk ? t('sites.nameBad') : undefined}
        onChange={(e) => {
          setTouched(true);
          setName(e.target.value.toLowerCase());
        }}
      />
      <div className="hub-stack">
        <span className="hub-section__t">{t('sites.access')}</span>
        <Segmented value={access} options={ACCESS_OPTIONS()} onChange={setAccess} label={t('sites.access')} />
        <Note>{access === 'team' ? t('sites.access.teamNote') : t('sites.access.linkNote')}</Note>
      </div>
      {error && <Msg tone="danger">{error}</Msg>}
      <div className="hub-row" data-end>
        <Button
          appearance="primary"
          size="l"
          loading={busy}
          disabled={!source || !nameOk}
          onClick={() => void publish()}
        >
          {progress && progress.total > 0
            ? t('sites.sending', { sent: progress.sent, total: progress.total })
            : t('sites.publish')}
        </Button>
      </div>
    </PlainPage>
  );
};

export const SiteScreen = ({ name }: { name: string }) => {
  const api = useApi();
  const env = useEnv();
  const { space } = useSpace();
  const [site, setSite] = useState<Site | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<number | 'access' | null>(null);
  const [removing, setRemoving] = useState(false);
  const [missing, setMissing] = useState(false);
  const { send, progress } = useSend();
  const [source, setSource] = useState<Source | null>(null);

  useEffect(() => {
    let alive = true;
    api.sites.get(space.slug, name).then(
      (s) => {
        if (alive) setSite(s);
      },
      (e: unknown) => {
        if (!alive) return;
        if (isApiError(e, 'not_found')) setMissing(true);
        else setError(siteErrorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api, space.slug, name]);

  const run = async (what: number | 'access', act: () => Promise<Site | void>) => {
    setBusy(what);
    setError(null);
    try {
      const next = await act();
      setSite(next ?? (await api.sites.get(space.slug, name)));
    } catch (e) {
      setError(siteErrorText(e));
    } finally {
      setBusy(null);
    }
  };

  const redeploy = (s: Source) => {
    setSource(s);
    void run('access', async () => {
      await send(name, s);
      setSource(null);
    });
  };

  const head = (
    <>
      <p className="hub-note">
        <Link to={`/s/${space.slug}`} className="hub-link">
          {t('sites.back', { name: space.name })}
        </Link>
      </p>
    </>
  );

  if (missing)
    return (
      <PlainPage side={<SpaceMenu current={space.slug} />}>
        {head}
        <EmptyState title={t('sites.gone.title')}>{t('sites.gone.text')}</EmptyState>
      </PlainPage>
    );
  if (!site)
    return (
      <PlainPage title={name} side={<SpaceMenu current={space.slug} />}>
        {head}
        {error ? <Msg tone="danger">{error}</Msg> : <ListSkeleton rows={3} />}
      </PlainPage>
    );

  return (
    <PlainPage title={site.name} side={<SpaceMenu current={space.slug} />}>
      {head}
      <div className="hub-stack">
        <CopyBox text={site.address} />
        <div className="hub-row">
          <Button
            appearance="primary"
            icon={<ExternalLink size={14} />}
            disabled={site.currentVersion === 0}
            onClick={() => env.assign(site.address)}
          >
            {t('sites.open')}
          </Button>
        </div>
      </div>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('sites.access')}</h2>
        <Segmented
          value={site.access}
          options={ACCESS_OPTIONS()}
          label={t('sites.access')}
          onChange={(access) => void run('access', () => api.sites.update(space.slug, site.name, { access }))}
        />
        <Note>{site.access === 'team' ? t('sites.access.teamNote') : t('sites.access.linkNote')}</Note>
      </section>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('sites.versions')}</h2>
        <div className="hub-list">
          {(site.versions ?? []).map((v) => (
            <div key={v.version} className="hub-item">
              <div className="hub-item__head">
                <span className="hub-t">
                  <span className="hub-t__n">{t('sites.version', { n: v.version })}</span>
                  <span className="hub-item__s">
                    {t('sites.versionMeta', {
                      when: whenText(v.createdAt),
                      who: v.createdBy,
                      size: formatBytes(v.totalBytes),
                    })}
                  </span>
                </span>
                {v.current ? (
                  <span className="hub-item__s">{t('sites.versionNow')}</span>
                ) : (
                  <Button
                    size="xs"
                    loading={busy === v.version}
                    disabled={busy !== null}
                    onClick={() =>
                      void run(v.version, () => api.sites.setCurrent(space.slug, site.name, v.version))
                    }
                  >
                    {t('sites.makeCurrent')}
                  </Button>
                )}
                <Button size="xs" appearance="ghost" onClick={() => env.assign(v.url)}>
                  {t('sites.open')}
                </Button>
              </div>
            </div>
          ))}
        </div>
        <Dropzone source={source} onPick={redeploy} />
        {progress && progress.total > 0 && (
          <Note>{t('sites.sending', { sent: progress.sent, total: progress.total })}</Note>
        )}
      </section>

      <TokensSection name={site.name} />

      {error && <Msg tone="danger">{error}</Msg>}
      <div className="hub-row">
        <Button appearance="outline" icon={<Trash2 size={14} />} onClick={() => setRemoving(true)}>
          {t('sites.remove')}
        </Button>
      </div>
      <RemoveDialog open={removing} name={site.name} onClose={() => setRemoving(false)} />
    </PlainPage>
  );
};

const TokensSection = ({ name }: { name: string }) => {
  const api = useApi();
  const { space } = useSpace();
  const [tokens, setTokens] = useState<SiteToken[] | null>(null);
  const [fresh, setFresh] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setTokens((await api.sites.tokens(space.slug, name)).tokens);
  }, [api, space.slug, name]);

  useEffect(() => {
    let alive = true;
    api.sites.tokens(space.slug, name).then(
      (r) => {
        if (alive) setTokens(r.tokens);
      },
      (e: unknown) => {
        if (alive) setError(siteErrorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api, space.slug, name]);

  const make = async () => {
    setBusy(true);
    setError(null);
    try {
      const made = await api.sites.newToken(space.slug, name, t('sites.tokenName'));
      setFresh(made.token ?? null);
      await load();
    } catch (e) {
      setError(siteErrorText(e));
    } finally {
      setBusy(false);
    }
  };

  const drop = async (id: string) => {
    setError(null);
    try {
      await api.sites.removeToken(space.slug, name, id);
      await load();
    } catch (e) {
      setError(siteErrorText(e));
    }
  };

  return (
    <section className="hub-section">
      <div className="hub-section__h">
        <h2 className="hub-section__t">{t('sites.tokens')}</h2>
        <Button size="s" loading={busy} icon={<Plus size={14} />} onClick={() => void make()}>
          {t('sites.tokenNew')}
        </Button>
      </div>
      <Note>{t('sites.tokensNote')}</Note>
      {fresh && (
        <div className="hub-stack">
          <Msg tone="warn">{t('sites.tokenOnce')}</Msg>
          <CopyBox text={fresh} wrap />
        </div>
      )}
      {tokens !== null && tokens.length > 0 && (
        <div className="hub-list">
          {tokens.map((k) => (
            <div key={k.id} className="hub-item">
              <div className="hub-item__head">
                <span className="hub-t">
                  <span className="hub-t__n">{k.name}</span>
                  <span className="hub-item__s">
                    {k.lastUsedAt
                      ? t('sites.tokenUsed', { when: whenText(k.lastUsedAt) })
                      : t('sites.tokenUnused', { when: whenText(k.createdAt) })}
                  </span>
                </span>
                <Button size="xs" appearance="ghost" onClick={() => void drop(k.id)}>
                  {t('sites.tokenRevoke')}
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
      {error && <Msg tone="danger">{error}</Msg>}
    </section>
  );
};

const RemoveDialog = ({ open, name, onClose }: { open: boolean; name: string; onClose: () => void }) => {
  const api = useApi();
  const { space } = useSpace();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const remove = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.sites.remove(space.slug, name);
      navigate(`/s/${space.slug}`);
    } catch (e) {
      setError(siteErrorText(e));
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      size="s"
      title={t('sites.remove.title', { name })}
      footer={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button appearance="primary" loading={busy} onClick={() => void remove()}>
            {t('sites.remove')}
          </Button>
        </>
      }
    >
      <p className="hub-note">{t('sites.remove.text')}</p>
      {error && <Msg tone="danger">{error}</Msg>}
    </Dialog>
  );
};
