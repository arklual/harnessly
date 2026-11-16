// The people who share one machine (PersonSettings.people): what each of them sees and hears of it. A project may be one
// person's (Project.owner) or everyone's; a chat is its project's, and a chat outside any project is its starter's
// (Conversation.startedBy). Everyone's things and one's own are in sight; another person's are folded away under their
// name until this browser unfolds them. What a chat answers is news for the one who wrote to it last
// (Conversation.by), whoever's chat it is. Nobody named in this browser, or a name the machine does not list: the
// page sees and hears everything, as before.

/** The people of this page: who sits here, among whom. */
export interface IPeopleView {
  me: string;
  people: readonly string[];
}

/** The view, or null while it is off (nobody named here, or a name the machine does not list). */
export const peopleView = (me: string | null, people: readonly string[]): IPeopleView | null =>
  me !== null && people.includes(me) ? { me, people } : null;

/** The owner of each project by its id: '' for everyone's. */
export const ownersOf = (projects: readonly { id: string; owner?: string }[]): ReadonlyMap<string, string> =>
  new Map(projects.map((p) => [p.id, p.owner ?? '']));

/** Whose a chat is: its project's owner ('' for everyone's project); outside any known project, its starter's. */
export const chatOwner = (
  chat: { projectId: string | null; startedBy?: string },
  owners: ReadonlyMap<string, string>,
): string => {
  if (chat.projectId !== null) {
    const owner = owners.get(chat.projectId);
    if (owner !== undefined) return owner;
  }
  return chat.startedBy ?? '';
};

/** In sight: everyone's, this person's, or of someone whose things this browser unfolded. */
export const inSight = (owner: string, view: IPeopleView, unfolded: ReadonlySet<string>): boolean =>
  owner === '' || owner === view.me || unfolded.has(owner);

/** What the chat answers is news for this person: they wrote to it last, or nobody is named and it is theirs or
 *  everyone's. */
export const newsFor = (by: string | undefined, owner: string, view: IPeopleView): boolean =>
  by ? by === view.me : owner === '' || owner === view.me;

/** A project as the sidebar draws it under the people: `band` is the heading it stands under. */
export interface IBandedProject {
  id: string;
  name: string;
  owner?: string;
  band?: string;
}

/** Another person's folded things: their projects, and their chats outside any project. */
export interface IPersonFold {
  person: string;
  projects: number;
  chats: number;
  open: boolean;
}

export interface IPeopleSplit<C, P> {
  chats: readonly C[];
  projects: readonly P[];
  folds: readonly IPersonFold[];
}

/**
 * The sidebar's lists for this person: everyone's projects first, then their own, then each unfolded person's, every
 * group under its heading (headings only once someone owns a project). Another person's chats and projects go out of
 * the lists unless unfolded; `keep` (the chat on screen) stays wherever it is. Each other person with anything gets a
 * fold, in the order of the machine's list, then by name.
 */
export const splitForPeople = <
  C extends { id: string; projectId: string | null; startedBy?: string },
  P extends { id: string; name: string; owner?: string },
>(
  chats: readonly C[],
  projects: readonly P[],
  view: IPeopleView,
  unfolded: ReadonlySet<string>,
  labels: { shared: string; mine: string },
  keep: string | null = null,
): IPeopleSplit<C, P & { band?: string }> => {
  const owners = ownersOf(projects);
  const banded = projects.some((p) => (p.owner ?? '') !== '');
  const others = new Map<string, { projects: number; chats: number }>();
  const other = (person: string) => {
    let f = others.get(person);
    if (!f) others.set(person, (f = { projects: 0, chats: 0 }));
    return f;
  };
  for (const p of projects) {
    const owner = p.owner ?? '';
    if (owner !== '' && owner !== view.me) other(owner).projects += 1;
  }
  const shownChats = chats.filter((chat) => {
    const owner = chatOwner(chat, owners);
    if (owner !== '' && owner !== view.me && (chat.projectId === null || !owners.has(chat.projectId)))
      other(owner).chats += 1;
    return chat.id === keep || inSight(owner, view, unfolded);
  });
  const rank = (person: string) => {
    const i = view.people.indexOf(person);
    return i < 0 ? view.people.length : i;
  };
  const order = [...others.keys()].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b));
  const group = (owner: string, band: string) =>
    projects.filter((p) => (p.owner ?? '') === owner).map((p) => (banded ? { ...p, band } : p));
  const shownProjects = [
    ...group('', labels.shared),
    ...group(view.me, labels.mine),
    ...order.filter((person) => unfolded.has(person)).flatMap((person) => group(person, person)),
  ];
  return {
    chats: shownChats,
    projects: shownProjects,
    folds: order.map((person) => ({
      person,
      ...(others.get(person) as { projects: number; chats: number }),
      open: unfolded.has(person),
    })),
  };
};
