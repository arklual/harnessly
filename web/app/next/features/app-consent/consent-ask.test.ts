import { describe, expect, it } from 'vitest';
import { consentAskOf, summaryOf } from '@/next/features/app-consent/consent-ask';
import type { IAccess, IInstalledApp } from '@/next/features/app-views/api';

const line = (key: string, cls: IAccess['class'], ru: string): IAccess => ({
  key,
  code: key,
  class: cls,
  label: { ru, en: ru },
});

const app = (over: Partial<IInstalledApp>): IInstalledApp =>
  ({ id: 'example.app', name: 'Example', version: '1.0.0', views: [], ...over }) as IInstalledApp;

describe('what a consent card asks (HRN-915)', () => {
  it('asks the whole list of a first install, and only what an update adds', () => {
    const all = [line('perm:files.read', 'medium', 'файлы'), line('net:example.com', 'high', 'сеть')];
    expect(consentAskOf(app({ consent: true, access: all }))).toMatchObject({
      version: '1.0.0',
      update: false,
    });
    expect(consentAskOf(app({ consent: true, access: all }))?.access).toHaveLength(2);
    const added = [line('secret:token', 'critical', 'секрет')];
    const update = consentAskOf(app({ access: all, pending: { version: '1.1.0', access: added } }));
    expect(update).toMatchObject({ version: '1.1.0', update: true });
    expect(update?.access).toEqual(added);
  });

  it('asks nothing of an app that waits for nothing', () => {
    expect(consentAskOf(app({ access: [] }))).toBeNull();
  });

  it('names the most dangerous lines first and counts the rest', () => {
    const access = [
      line('a', 'low', 'тема'),
      line('b', 'critical', 'секрет'),
      line('c', 'medium', 'файлы'),
      line('d', 'high', 'сеть'),
      line('e', 'low', 'язык'),
    ];
    const text = summaryOf(access);
    expect(text.indexOf('секрет')).toBeLessThan(text.indexOf('сеть'));
    expect(text.indexOf('сеть')).toBeLessThan(text.indexOf('файлы'));
    expect(text).not.toContain('тема');
    expect(text).toContain('2');
  });
});
