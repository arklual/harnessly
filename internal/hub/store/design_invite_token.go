package store

import "regexp"

// TeamDesignInvitePrefix separates team-only links from old unscoped links.
// A previous binary cannot redeem a team-only link as an unscoped invitation.
const TeamDesignInvitePrefix = "team-"

var designInviteToken = regexp.MustCompile(`^(?:` + TeamDesignInvitePrefix + `)?[A-Za-z0-9_-]{43}$`)

func ValidDesignInviteToken(token string) bool { return designInviteToken.MatchString(token) }
