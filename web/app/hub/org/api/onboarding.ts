// The onboarding step «Организация» (HRN-536): POST /api/orgs makes the organization and moves the chosen spaces in
// (docs/hub/orgs.md); GET /api/orgs lists `looseSpaces`, the person's spaces that are in no organization.
export interface LooseSpace {
  id: string;
  slug: string;
  name: string;
  /** Only an owner may move a space into an organization. */
  owner: boolean;
}

export interface OnboardingApi {
  looseSpaces(): Promise<LooseSpace[]>;
  /** Makes the organization with the author as its admin; resolves with its slug. */
  createOrg(input: {
    name: string;
    spaceIds: string[];
    /** The first tenant, made when no space moves in (empty: the hub's default name); an organization is never empty. */
    tenantName?: string;
  }): Promise<{ slug: string }>;
}
