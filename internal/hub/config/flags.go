package config

import (
	"sort"
	"strings"
	"time"
)

// Flag is one environment variable the hub's configuration reads.
type Flag struct {
	// Name is the variable. A name that ends in "_" stands for a family: every variable that starts with it
	// (HUB_OIDC_<PROVIDER>_ISSUER and the like).
	Name string
	// Owner is the area of the hub that answers for the variable.
	Owner string
	// Temporary marks a switch that exists to roll a behaviour out and goes away once every installation runs with it
	// on; a lasting setting an installation keeps choosing is not temporary.
	Temporary bool
	// RemoveAfter is the day (YYYY-MM-DD) after which a temporary switch is due to be removed. A temporary switch
	// without it fails the package's tests, and so does a date on a lasting setting.
	RemoveAfter string
}

// Flags is the registry of every variable Load reads, sorted by owner and name. A variable Load reads and this table
// does not name fails the package's tests, and so does a row nothing reads. Every variable read through NAME is also
// read through NAME_FILE (the value comes from the file), which the table does not repeat.
var Flags = []Flag{
	{Name: "HUB_ACCESS_ENFORCE", Owner: "access", Temporary: true, RemoveAfter: "2027-02-01"},
	{Name: "HUB_MAX_ORGS_PER_PERSON", Owner: "access"},
	{Name: "HUB_MAX_SERVERS_PER_SPACE", Owner: "access"},
	{Name: "HUB_MAX_SPACES_PER_PERSON", Owner: "access"},
	{Name: "HUB_ORGS", Owner: "access", Temporary: true, RemoveAfter: "2027-02-01"},
	{Name: "HUB_RESOURCE_ACCESS", Owner: "access", Temporary: true, RemoveAfter: "2027-02-01"},
	{Name: "HUB_SWITCHES", Owner: "access"},
	{Name: "HUB_EFFICIENCY_MARKERS", Owner: "analytics"},
	{Name: "HUB_EFFICIENCY_REGRESSIONS", Owner: "analytics"},
	{Name: "HUB_EFFICIENCY_TRACKER_PROJECT", Owner: "analytics"},
	{Name: "HUB_EFFICIENCY_TRACKER_TOKEN", Owner: "analytics"},
	{Name: "HUB_EFFICIENCY_TRACKER_URL", Owner: "analytics"},
	{Name: "HUB_MR_SOURCE_GITLAB_GROUPS", Owner: "analytics"},
	{Name: "HUB_MR_SOURCE_GITLAB_SKILLS_PROJECT", Owner: "analytics"},
	{Name: "HUB_MR_SOURCE_GITLAB_TOKEN", Owner: "analytics"},
	{Name: "HUB_MR_SOURCE_GITLAB_URL", Owner: "analytics"},
	{Name: "HUB_RATES_URL", Owner: "analytics"},
	{Name: "HUB_USAGE_INGEST", Owner: "analytics", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_APPS_GUEST_URL", Owner: "apps"},
	{Name: "HUB_APP_TOKEN", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_AUDIENCE", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_AUDIENCES", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_CLIENT_ID", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_CLIENT_SECRET", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_SCOPE", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_SUBJECT_TOKEN_TYPE", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_EXCHANGE_URL", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_KEY_FILE", Owner: "apps"},
	{Name: "HUB_APP_TOKEN_PREVIOUS_KEYS", Owner: "apps"},
	{Name: "HUB_EXTENSIONS", Owner: "apps"},
	{Name: "HUB_EXTENSIONS_FILE", Owner: "apps"},
	{Name: "HUB_ADMIN_EMAILS", Owner: "auth"},
	{Name: "HUB_AUTH_EMAIL", Owner: "auth"},
	{Name: "HUB_GITHUB_CLIENT_ID", Owner: "auth"},
	{Name: "HUB_GITHUB_CLIENT_SECRET", Owner: "auth"},
	{Name: "HUB_GITHUB_URL", Owner: "auth"},
	{Name: "HUB_LOCAL_CODE_FILE", Owner: "auth"},
	{Name: "HUB_LOCAL_CODE_GROUP", Owner: "auth"},
	{Name: "HUB_OIDC_", Owner: "auth"},
	{Name: "HUB_OIDC_EMAIL_TRUSTED", Owner: "auth"},
	{Name: "HUB_OIDC_PROVIDERS", Owner: "auth"},
	{Name: "HUB_PASSKEY", Owner: "auth"},
	{Name: "HUB_PROXY_JWT_AUDIENCE", Owner: "auth"},
	{Name: "HUB_PROXY_JWT_EMAIL_CLAIM", Owner: "auth"},
	{Name: "HUB_PROXY_JWT_HEADER", Owner: "auth"},
	{Name: "HUB_PROXY_JWT_HOST", Owner: "auth"},
	{Name: "HUB_PROXY_JWT_ISSUER", Owner: "auth"},
	{Name: "HUB_PROXY_JWT_JWKS_URL", Owner: "auth"},
	{Name: "HUB_RP_ID", Owner: "auth"},
	{Name: "HUB_RP_NAME", Owner: "auth"},
	{Name: "HUB_SIGNUP", Owner: "auth"},
	{Name: "HUB_YANDEX_CLIENT_ID", Owner: "auth"},
	{Name: "HUB_YANDEX_CLIENT_SECRET", Owner: "auth"},
	{Name: "HUB_AUTOMATIONS", Owner: "automations", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_AUTOMATIONS_GITLAB_TOKEN", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_GITLAB_URL", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_MR_PROJECTS", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_NOTIFY_TOKEN", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_NOTIFY_URL", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_PORT_ENV", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_SECRET", Owner: "automations"},
	{Name: "HUB_AUTOMATIONS_TOKEN_ENV", Owner: "automations"},
	{Name: "HUB_LAUNCH_TOKEN", Owner: "automations"},
	{Name: "HUB_DATA_DIR", Owner: "core"},
	{Name: "HUB_DIST_DIR", Owner: "core"},
	{Name: "HUB_EDITION", Owner: "core"},
	{Name: "HUB_FEATURES_DEFAULT", Owner: "core"},
	{Name: "HUB_FEATURES_FORCE", Owner: "core"},
	{Name: "HUB_LISTEN", Owner: "core"},
	{Name: "HUB_PUBLIC_URL", Owner: "core"},
	{Name: "HUB_SECRET_KEY", Owner: "core"},
	{Name: "HUB_TERMS_URL", Owner: "core"},
	{Name: "HUB_TRUSTED_PROXIES", Owner: "core"},
	{Name: "HUB_WEB_DIR", Owner: "core"},
	{Name: "HUB_DATABASE_URL", Owner: "db"},
	{Name: "HUB_DB_CHECK", Owner: "db"},
	{Name: "HUB_DB_MIGRATE_URL", Owner: "db"},
	{Name: "HUB_DB_POOLER", Owner: "db"},
	{Name: "HUB_DB_SCHEMA", Owner: "db"},
	{Name: "HUB_DESIGN_IMPORT_DEFAULT_MACHINE", Owner: "design"},
	{Name: "HUB_DESIGN_IMPORT_GATEWAY", Owner: "design", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_DESIGN_IMPORT_GATEWAY_URL", Owner: "design"},
	{Name: "HUB_DESIGN_IMPORT_MACHINES", Owner: "design"},
	{Name: "HUB_DESIGN_IMPORT_MACHINE_PREFIX", Owner: "design"},
	{Name: "HUB_DESIGN_IMPORT_SPACE", Owner: "design"},
	{Name: "HUB_DESIGN_IMPORT_TEAMS", Owner: "design"},
	{Name: "HUB_DESIGN_PLATFORM_API", Owner: "design"},
	{Name: "HUB_DESIGN_SHARE_ALIASES", Owner: "design"},
	{Name: "HUB_DESIGN_SHARE_DOCS", Owner: "design"},
	{Name: "HUB_DESIGN_SHARE_URL", Owner: "design"},
	{Name: "HUB_ARTIFACTS_ACCESS_TOKEN", Owner: "docs"},
	{Name: "HUB_ARTIFACTS_PUBLIC_URL", Owner: "docs"},
	{Name: "HUB_ARTIFACTS_TOKEN", Owner: "docs"},
	{Name: "HUB_ARTIFACTS_URL", Owner: "docs"},
	{Name: "HUB_BOARDS_PUBLIC_URL", Owner: "docs"},
	{Name: "HUB_BOARDS_TOKEN", Owner: "docs"},
	{Name: "HUB_BOARDS_URL", Owner: "docs"},
	{Name: "HUB_BOARD_COMMAND", Owner: "docs"},
	{Name: "HUB_BOARD_EVENTS_FORWARD_URL", Owner: "docs"},
	{Name: "HUB_BOARD_EVENTS_SECRET", Owner: "docs"},
	{Name: "HUB_DOCS", Owner: "docs", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_DOCS_LEGACY_HOSTS", Owner: "docs"},
	{Name: "HUB_ENV_FEATURES", Owner: "environment", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_ENV_RECOMMENDED", Owner: "environment"},
	{Name: "HUB_HISTORY", Owner: "history", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_HISTORY_DELETED_DAYS", Owner: "history"},
	{Name: "HUB_LIMITS_ENFORCE", Owner: "limits", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_AGENT_URL", Owner: "link"},
	{Name: "HUB_DRAIN_LINKS", Owner: "link"},
	{Name: "HUB_DRAIN_SETTLE", Owner: "link"},
	{Name: "HUB_MIN_AGENT_VERSION", Owner: "link"},
	{Name: "HUB_REPLICAS", Owner: "link"},
	{Name: "HUB_REPLICA_ADDR", Owner: "link"},
	{Name: "HUB_REPLICA_ID", Owner: "link"},
	{Name: "HUB_REPLICA_LISTEN", Owner: "link"},
	{Name: "HUB_REPLICA_SECRET", Owner: "link"},
	{Name: "HUB_TUNNEL_MEMORY_MB", Owner: "link"},
	{Name: "HUB_ALLOW_SAME_SITE_RUNTIME", Owner: "machines"},
	{Name: "HUB_CONTAINER_IMAGE", Owner: "machines"},
	{Name: "HUB_MACHINE_RESOURCES", Owner: "machines", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_MACHINE_TRUST", Owner: "machines"},
	{Name: "HUB_PREVIEW_SLOT_DOMAIN", Owner: "machines"},
	{Name: "HUB_PREVIEW_SLOT_PREFIX", Owner: "machines"},
	{Name: "HUB_PROVIDER", Owner: "machines"},
	{Name: "HUB_PROVIDER_PROTOCOL", Owner: "machines", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_PROVIDER_TOKEN_FILE", Owner: "machines"},
	{Name: "HUB_PROVIDER_URL", Owner: "machines"},
	{Name: "HUB_RUNTIME_DOMAIN", Owner: "machines"},
	{Name: "HUB_SSH", Owner: "machines"},
	{Name: "HUB_SSH_ALLOW_PRIVATE", Owner: "machines"},
	{Name: "HUB_MAIL_FROM", Owner: "mail"},
	{Name: "HUB_MAIL_LOG", Owner: "mail"},
	{Name: "HUB_SMTP_URL", Owner: "mail"},
	{Name: "HUB_MEMORY_SYNC", Owner: "memory", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_LOG_LEVEL", Owner: "ops"},
	{Name: "HUB_METRICS_LISTEN", Owner: "ops"},
	{Name: "HUB_SHUTDOWN_GRACE", Owner: "ops"},
	{Name: "HUB_PEERS", Owner: "peers", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_SECRETS_REF_PREFIXES", Owner: "safe"},
	{Name: "HUB_SECRETS_VAULT_", Owner: "safe"},
	{Name: "HUB_SECRETS_VAULT_ADDR", Owner: "safe"},
	{Name: "HUB_SECRETS_VAULT_PREFIX", Owner: "safe"},
	{Name: "HUB_VAULT_KEYS", Owner: "safe"},
	{Name: "HUB_VAULT_SHARED_EDITORS", Owner: "safe"},
	{Name: "HUB_VAULT_SHARED_MEMBERS", Owner: "safe"},
	{Name: "HARNESSLY_FLEET_SITES_SECRET", Owner: "sites"},
	{Name: "HARNESSLY_FLEET_SITES_SPACE", Owner: "sites"},
	{Name: "HARNESSLY_SITES", Owner: "sites"},
	{Name: "HARNESSLY_SITES_ABUSE_URL", Owner: "sites"},
	{Name: "HARNESSLY_SITES_DIR", Owner: "sites"},
	{Name: "HARNESSLY_SITES_DOMAIN", Owner: "sites"},
	{Name: "HARNESSLY_SITES_S3_ACCESS_KEY", Owner: "sites"},
	{Name: "HARNESSLY_SITES_S3_BUCKET", Owner: "sites"},
	{Name: "HARNESSLY_SITES_S3_ENDPOINT", Owner: "sites"},
	{Name: "HARNESSLY_SITES_S3_PREFIX", Owner: "sites"},
	{Name: "HARNESSLY_SITES_S3_REGION", Owner: "sites"},
	{Name: "HARNESSLY_SITES_S3_SECRET_KEY", Owner: "sites"},
	{Name: "HARNESSLY_SITES_UPSTREAM_PEOPLE", Owner: "sites"},
	{Name: "HARNESSLY_SITES_UPSTREAM_SECRET", Owner: "sites"},
	{Name: "HARNESSLY_SITES_UPSTREAM_STRIP", Owner: "sites"},
	{Name: "HARNESSLY_SITES_UPSTREAM_URL", Owner: "sites"},
	{Name: "HUB_S3_ACCESS_KEY", Owner: "storage"},
	{Name: "HUB_S3_BUCKET", Owner: "storage"},
	{Name: "HUB_S3_ENDPOINT", Owner: "storage"},
	{Name: "HUB_S3_PREFIX", Owner: "storage"},
	{Name: "HUB_S3_REGION", Owner: "storage"},
	{Name: "HUB_S3_SECRET_KEY", Owner: "storage"},
	{Name: "HUB_STORAGE", Owner: "storage"},
	{Name: "HUB_STORE", Owner: "store", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_STORE_DATABASE_URL", Owner: "store"},
	{Name: "HUB_STORE_DIR", Owner: "store"},
	{Name: "HUB_STORE_LEGACY_SPACE", Owner: "store", Temporary: true, RemoveAfter: "2027-03-01"},
	{Name: "HUB_STORE_MAX_UPLOAD_MB", Owner: "store"},
	{Name: "HUB_STORE_PROFILE_ROOMS", Owner: "store"},
	{Name: "HUB_STORE_S3_ACCESS_KEY", Owner: "store"},
	{Name: "HUB_STORE_S3_BUCKET", Owner: "store"},
	{Name: "HUB_STORE_S3_ENDPOINT", Owner: "store"},
	{Name: "HUB_STORE_S3_PREFIX", Owner: "store"},
	{Name: "HUB_STORE_S3_REGION", Owner: "store"},
	{Name: "HUB_STORE_S3_SECRET_KEY", Owner: "store"},
	{Name: "HUB_VOICE_TAKES", Owner: "voice", Temporary: true, RemoveAfter: "2027-03-01"},
}

// KnownFlag says whether the registry names the variable: by itself, as the _FILE form of a row, or as a member of a
// family.
func KnownFlag(name string) bool {
	for _, f := range Flags {
		if strings.HasSuffix(f.Name, "_") {
			if strings.HasPrefix(name, f.Name) && len(name) > len(f.Name) {
				return true
			}
			continue
		}
		if f.Name == name || f.Name+"_FILE" == name {
			return true
		}
	}
	return false
}

// OverdueFlags are the temporary switches whose removal date has passed by now, sorted by name.
func OverdueFlags(now time.Time) []Flag {
	var out []Flag
	for _, f := range Flags {
		if !f.Temporary {
			continue
		}
		if day, err := time.Parse(time.DateOnly, f.RemoveAfter); err == nil && now.After(day.AddDate(0, 0, 1)) {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
