/**
 * GitLab hosts whose merge requests and commits the page shows (RUNTIME_GITLAB_HOSTS; gitlab.com by default). Nothing
 * else is a GitLab: a link has to be https on exactly one of these hosts.
 */
import { gitlabHosts } from '@/lib/site';

export const GITLAB_COM_HOST = 'gitlab.com';

export type TGitlabHost = string;

export const isGitlabHost = (host: string): host is TGitlabHost => gitlabHosts().includes(host);

export const gitlabApiOf = (host: TGitlabHost): string => `https://${host}/api/v4`;

/** The URL when it is https on a GitLab host with no credentials or port; null for anything else. */
export const gitlabUrlOf = (raw: string): URL | null => {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || !isGitlabHost(url.hostname))
    return null;
  return url;
};
