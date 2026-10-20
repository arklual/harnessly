const BUILD_ID_RE = /"buildId":"([A-Za-z0-9_-]{1,64})"/;

/** The build a served page was made by, read from its `__NEXT_DATA__`. */
export const buildIdOfPage = (html: string): string | null => BUILD_ID_RE.exec(html)?.[1] ?? null;

/** A newer build is out when the server's page names a build and it is not the one running here. */
export const isNewerBuild = (running: string | null | undefined, served: string | null): boolean =>
  Boolean(running && served && running !== served);
