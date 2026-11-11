import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  askMessage,
  askRef,
  changeLinkOf,
  changeTitle,
  pickQuery,
  readPick,
  type IPickMr,
} from '@/next/features/review/gitlab-pick';

beforeAll(() => pinLocale('en'));

describe('changeLinkOf', () => {
  it('reads a merge request and a commit of a GitLab', () => {
    expect(changeLinkOf(' https://gitlab.example.com/team/app/-/merge_requests/12/diffs?x=1 ')).toEqual({
      url: 'https://gitlab.example.com/team/app/-/merge_requests/12',
      kind: 'mr',
      github: false,
      host: 'gitlab.example.com',
      project: 'team/app',
      ref: '12',
    });
    expect(changeLinkOf('https://gitlab.example.com/g/s/app/-/commit/0123abc')).toMatchObject({
      kind: 'commit',
      project: 'g/s/app',
      ref: '0123abc',
    });
  });

  it('reads a pull request and a commit of a GitHub', () => {
    expect(changeLinkOf('https://github.com/acme/app/pull/7/files')).toEqual({
      url: 'https://github.com/acme/app/pull/7',
      kind: 'mr',
      github: true,
      host: 'github.com',
      project: 'acme/app',
      ref: '7',
    });
    expect(changeLinkOf('https://github.com/acme/app/commit/0123abc')).toMatchObject({
      kind: 'commit',
      github: true,
      project: 'acme/app',
    });
    expect(changeLinkOf('https://github.com/acme/app/issues/7')).toBeNull();
    const pull = changeLinkOf('https://github.com/acme/app/pull/7');
    expect(pull && changeTitle(pull)).toBe('app #7');
  });

  it('takes nothing else', () => {
    expect(changeLinkOf('http://gitlab.example.com/team/app/-/merge_requests/1')).toBeNull();
    expect(changeLinkOf('https://u:p@gitlab.example.com/team/app/-/merge_requests/1')).toBeNull();
    expect(changeLinkOf('https://example.com/team/app')).toBeNull();
    expect(changeLinkOf('not a link')).toBeNull();
  });

  it('names the tab by the repository', () => {
    const mr = changeLinkOf('https://gitlab.example.com/team/app/-/merge_requests/12');
    const commit = changeLinkOf('https://gitlab.example.com/team/app/-/commit/0123456789abcdef');
    expect(mr && changeTitle(mr)).toBe('app !12');
    expect(commit && changeTitle(commit)).toBe('app 01234567');
  });
});

describe('readPick', () => {
  it('keeps the items it understands', () => {
    const answer = readPick<IPickMr>((v) => {
      const o = v as Record<string, unknown>;
      return typeof o.iid === 'number' ? (o as unknown as IPickMr) : null;
    })({ state: 'ready', items: [{ iid: 1 }, { nope: true }] });
    expect(answer.state).toBe('ready');
    expect(answer.items).toHaveLength(1);
    expect(answer.reason).toBeNull();
  });

  it('refuses an answer of another shape', () => {
    expect(() => readPick(() => null)({ items: [] })).toThrow();
  });
});

describe('pickQuery', () => {
  it('sends only what the list needs', () => {
    expect(pickQuery({ list: 'projects', q: '  ' })).toBe('list=projects');
    expect(pickQuery({ list: 'mrs', project: 'team/app', filter: 'review', q: ' 42 ' })).toBe(
      'list=mrs&project=team%2Fapp&filter=review&q=42',
    );
    expect(pickQuery({ list: 'commits', project: 'team/app', ref: 'feat/x' })).toBe(
      'list=commits&project=team%2Fapp&ref=feat%2Fx',
    );
  });
});

describe('asking the agent', () => {
  const link = changeLinkOf('https://gitlab.example.com/team/app/-/merge_requests/12');
  it('names the change, its title and branches, then the words', () => {
    if (!link) throw new Error('link');
    const ctx = { link, title: 'Queue', source: 'feat', target: 'main' };
    expect(askMessage(ctx, ' why? ')).toBe(
      'Merge request !12 in team/app: https://gitlab.example.com/team/app/-/merge_requests/12\nTitle: Queue\nBranches: feat → main\n\nwhy?',
    );
    expect(askRef(ctx)).toEqual({
      kind: 'mr',
      id: link.url,
      title: 'app !12',
      target: link.url,
    });
  });

  it('says only what it knows', () => {
    if (!link) throw new Error('link');
    expect(askMessage({ link, title: null, source: null, target: null }, '')).toBe(
      'Merge request !12 in team/app: https://gitlab.example.com/team/app/-/merge_requests/12',
    );
  });
});
