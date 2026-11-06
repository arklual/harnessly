// A call the guard of a chat's apps refused (internal/hlcli app.go appGuard): the agent gets the reason with a mark at
// its head, `[app-guard "<app name>" "<rule>"] <words>`, and the feed draws the failed tool as one quiet line.

export interface IGuardDenial {
  /** The name of the app that refused; empty for a refusal no single app made (the apps' own files and commands). */
  app: string;
  rule: string;
  /** The words for the agent, without the mark. */
  reason: string;
}

const MARK = /^\s*\[app-guard ("(?:[^"\\]|\\.)*") ("(?:[^"\\]|\\.)*")\] ?/;

export const parseGuardDenial = (text: string | undefined): IGuardDenial | null => {
  const found = text ? MARK.exec(text) : null;
  if (!found) return null;
  try {
    const app: unknown = JSON.parse(found[1]);
    const rule: unknown = JSON.parse(found[2]);
    if (typeof app !== 'string' || typeof rule !== 'string') return null;
    return { app, rule, reason: text!.slice(found[0].length) };
  } catch {
    return null;
  }
};
