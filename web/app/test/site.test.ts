import { describe, expect, it } from 'vitest';
import { githubIssueUrl, githubRefOf, issueUrlOf } from '@/lib/site';
import { isTaskKey, taskKeyOf } from '@/lib/hl-task';
import { presentLink } from '@/lib/presented-link';

describe('GitHub links and keys', () => {
  it('reads issues and pull requests of the configured GitHub only', () => {
    expect(githubRefOf('https://github.com/acme/app/issues/12')).toEqual({
      owner: 'acme',
      repo: 'app',
      number: 12,
      pull: false,
    });
    expect(githubRefOf('https://github.com/acme/app/pull/7/')).toEqual({
      owner: 'acme',
      repo: 'app',
      number: 7,
      pull: true,
    });
    expect(githubRefOf('https://github.com/acme/app/issues/12/files')).toBeNull();
    expect(githubRefOf('https://evil.example/acme/app/issues/12')).toBeNull();
    expect(githubRefOf('http://github.com/acme/app/issues/12')).toBeNull();
  });

  it('treats owner/repo#n as a task key with its page', () => {
    expect(isTaskKey('acme/app#12')).toBe(true);
    expect(isTaskKey('acme/app#0')).toBe(false);
    expect(taskKeyOf('https://github.com/acme/app/issues/12')).toBe('acme/app#12');
    expect(taskKeyOf('https://github.com/acme/app/pull/7')).toBeNull();
    expect(githubIssueUrl('acme/app#12')).toBe('https://github.com/acme/app/issues/12');
    expect(issueUrlOf('acme/app#12')).toBe('https://github.com/acme/app/issues/12');
    expect(issueUrlOf('ABC-1')).toBeNull();
  });

  it('presents a pull request like a merge request and an issue like a task', () => {
    expect(presentLink('https://github.com/acme/app/pull/7')).toMatchObject({
      kind: 'mr',
      iid: 7,
      project: 'acme/app',
    });
    expect(presentLink('https://github.com/acme/app/issues/12')).toMatchObject({
      kind: 'issue',
      key: 'acme/app#12',
    });
    expect(presentLink('https://github.com/acme/app')).toMatchObject({ kind: 'github' });
  });
});
