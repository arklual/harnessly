// «Ссылки в чате» in the organization's settings: which addresses the chat tells as stands, tracker tasks,
// GitLab, documents and boards. The hub keeps them after it checked them and sends them to the machines with their list.
import { useEffect, useState } from 'react';

import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { Btn } from '../ui';
import { formOfLinks, linksChanged, linksOfForm, type LinksForm } from './settings-links';

type Loaded = { saved: LinksForm } | { error: string };

type Field = {
  key: keyof LinksForm;
  label: string;
  hint: string;
  placeholder: string;
};

const FIELDS: Field[] = [
  {
    key: 'standDomain',
    label: 'Домен стендов',
    hint: 'Адреса вида имя.домен открываются в браузере панели как стенды.',
    placeholder: 'preview.example.com',
  },
  {
    key: 'trackerUrl',
    label: 'Трекер',
    hint: 'Номера задач в чате становятся ссылками на этот адрес.',
    placeholder: 'https://tracker.example.com',
  },
  {
    key: 'gitlabHosts',
    label: 'GitLab',
    hint: 'Хосты через запятую: ссылки на них в чате узнаются как GitLab.',
    placeholder: 'gitlab.example.com',
  },
  {
    key: 'artifactsUrl',
    label: 'Документы',
    hint: 'Адрес, где лежат документы команды.',
    placeholder: 'https://a.example.com',
  },
  {
    key: 'boardsUrl',
    label: 'Доски',
    hint: 'Адрес, где лежат доски команды.',
    placeholder: 'https://b.example.com',
  },
];

export const LinksSection = () => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  // null while it loads: fields drawn before the answer would show addresses the hub may not have.
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [form, setForm] = useState<LinksForm | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    api.links(org).then(
      (links) => {
        if (!live) return;
        const saved = formOfLinks(links);
        setLoaded({ saved });
        setForm(saved);
      },
      (e: unknown) => live && setLoaded({ error: orgErrorText(e) }),
    );
    return () => {
      live = false;
    };
  }, [api, org]);

  const saved = loaded && 'saved' in loaded ? loaded.saved : null;
  const dirty = saved !== null && form !== null && linksChanged(form, saved);
  const save = () => {
    if (form === null || !dirty || busy) return;
    setBusy(true);
    setFailed(null);
    api.setLinks(org, linksOfForm(form)).then(
      (links) => {
        const next = formOfLinks(links);
        setLoaded({ saved: next });
        setForm(next);
        setBusy(false);
      },
      (e: unknown) => {
        setFailed(orgErrorText(e));
        setBusy(false);
      },
    );
  };

  return (
    <>
      <h2 className="hd-sech" data-sec="links">
        Ссылки в чатах
        <span className="mut2 sm">Адреса, которые агенты превращают в ссылки в чатах.</span>
      </h2>
      {form !== null && (
        <>
          <div className="t-card viz-card dash-tile hd-card flat">
            {FIELDS.map((f) => (
              <label key={f.key} className="set" data-busy={busy ? 'true' : undefined}>
                <div>
                  <div className="hd-t">{f.label}</div>
                  <div className="hd-d">{f.hint}</div>
                </div>
                <div>
                  <input
                    className="inp"
                    style={{ width: 280 }}
                    value={form[f.key]}
                    maxLength={300}
                    placeholder={f.placeholder}
                    spellCheck={false}
                    autoComplete="off"
                    onChange={(e) => setForm({ ...form, [f.key]: e.target.value })}
                  />
                </div>
              </label>
            ))}
          </div>
          <div style={{ marginTop: 12 }}>
            <Btn className="hd-btn" variant="primary" size="sm" busy={busy} disabled={!dirty} onClick={save}>
              Сохранить
            </Btn>
          </div>
        </>
      )}
      {loaded && 'error' in loaded && (
        <p className="mut sm" style={{ marginTop: 8 }}>
          {loaded.error}
        </p>
      )}
      {failed && (
        <p className="mut sm" style={{ marginTop: 8 }}>
          {failed}
        </p>
      )}
    </>
  );
};
