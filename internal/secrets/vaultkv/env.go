package vaultkv

import (
	"errors"
	"strconv"
	"strings"
)

// K8sTokenFile is the pod's service account token, the kubernetes method's default.
const K8sTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// FromEnv reads the settings of a server under one prefix of names (RUNTIME_SECRETS_VAULT_, HUB_SECRETS_VAULT_):
//
//	ADDR, MOUNT, NAMESPACE, KV_VERSION, CACERT   where the server is
//	AUTH                                         token-file (default), approle, kubernetes or jwt
//	TOKEN_FILE                                   token-file: the file with the token
//	ROLE_ID, SECRET_ID_FILE                      approle
//	ROLE, JWT_FILE                               kubernetes (JWT_FILE defaults to the pod's token) and jwt
//	AUTH_MOUNT                                   where the login lives, when not at its method's name
//
// The error names the setting to fix and never a value.
func FromEnv(prefix string, getenv func(string) string) (Config, error) {
	env := func(k string) string { return strings.TrimSpace(getenv(prefix + k)) }
	cfg := Config{Addr: env("ADDR"), Mount: env("MOUNT"), Namespace: env("NAMESPACE"), CACert: env("CACERT")}
	if v := env("KV_VERSION"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || (n != 1 && n != 2) {
			return Config{}, errors.New(prefix + "KV_VERSION must be 1 or 2")
		}
		cfg.KVVersion = n
	}
	mount := env("AUTH_MOUNT")
	switch method := env("AUTH"); method {
	case "", "token-file":
		if env("TOKEN_FILE") == "" {
			return Config{}, errors.New(prefix + "TOKEN_FILE is not set")
		}
		cfg.Auth = TokenFile{Path: env("TOKEN_FILE")}
	case "approle":
		if env("ROLE_ID") == "" || env("SECRET_ID_FILE") == "" {
			return Config{}, errors.New("approle needs " + prefix + "ROLE_ID and " + prefix + "SECRET_ID_FILE")
		}
		cfg.Auth = AppRole{Mount: mount, RoleID: env("ROLE_ID"), SecretIDFile: env("SECRET_ID_FILE")}
	case "kubernetes", "jwt":
		file := env("JWT_FILE")
		if file == "" && method == "kubernetes" {
			file = K8sTokenFile
		}
		if env("ROLE") == "" || file == "" {
			return Config{}, errors.New(method + " needs " + prefix + "ROLE and " + prefix + "JWT_FILE")
		}
		cfg.Auth = JWT{Kind: method, Mount: mount, Role: env("ROLE"), TokenFile: file}
	default:
		return Config{}, errors.New(prefix + "AUTH must be token-file, approle, kubernetes or jwt")
	}
	return cfg, nil
}

// SplitPrefixes reads a comma-separated list of folders (the references a safe may read), trimmed of slashes.
func SplitPrefixes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.Trim(strings.TrimSpace(p), "/"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
