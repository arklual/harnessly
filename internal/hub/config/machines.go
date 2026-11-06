package config

import (
	"strings"
)

// Modes of HUB_ACCESS_ENFORCE.
const (
	// AccessOff does not check a machine's access list when a person opens the machine (the behaviour before the setting).
	AccessOff = "off"
	// AccessWarn checks, logs what would have been refused, and lets the person in.
	AccessWarn = "warn"
	// AccessOn checks and refuses.
	AccessOn = "on"
)

// MachinesConfig is what the machine resources, operations and access are set by (docs/hub/machines-resources.md).
type MachinesConfig struct {
	// Provider (HUB_PROVIDER) is none or http; empty in the environment means http when ProviderURL is set, else none.
	Provider string
	// ProviderURL (HUB_PROVIDER_URL) is the base address of the provider service (docs/hub/provider.md).
	ProviderURL string
	// ProviderTokenFile (HUB_PROVIDER_TOKEN_FILE) is a file with the bearer token the provider is called with. It is the
	// path, not the content, because the hub reads the file again when it changes.
	ProviderTokenFile string
	// ProviderProtocol (HUB_PROVIDER_PROTOCOL) is v0 (the default: the protocol without a version, as before), auto (ask
	// GET /v1/describe, speak v1 when the service answers it) or v1 (the service must speak v1). Only v1 gives create,
	// attach and the provider endpoints of docs/hub/provider.md.
	ProviderProtocol string
	// AccessEnforce (HUB_ACCESS_ENFORCE) is off, warn or on: whether the access list of a machine is kept when someone
	// who is not its owner opens it.
	AccessEnforce string
	// Resources (HUB_MACHINE_RESOURCES=on) opens the resources, load and operations of machines to every organization;
	// off (the default) leaves them to the organization's own feature, and with no feature store they stay closed.
	Resources bool
}

func loadMachines(lookup func(string) (string, bool), def func(string, string) string, bad func(string, ...any)) MachinesConfig {
	var m MachinesConfig
	m.ProviderURL = def("HUB_PROVIDER_URL", "")
	if v, ok := lookup("HUB_PROVIDER_TOKEN_FILE"); ok {
		m.ProviderTokenFile = strings.TrimSpace(v)
	}
	m.Provider = strings.ToLower(def("HUB_PROVIDER", ""))
	switch m.Provider {
	case "":
		m.Provider = "none"
		if m.ProviderURL != "" {
			m.Provider = "http"
		}
	case "none":
	case "http":
		if m.ProviderURL == "" {
			bad("HUB_PROVIDER=http needs HUB_PROVIDER_URL")
		}
	default:
		bad("HUB_PROVIDER must be none or http, got %q", m.Provider)
	}
	switch m.ProviderProtocol = strings.ToLower(def("HUB_PROVIDER_PROTOCOL", "v0")); m.ProviderProtocol {
	case "v0", "auto", "v1":
	default:
		bad("HUB_PROVIDER_PROTOCOL must be v0, auto or v1, got %q", m.ProviderProtocol)
	}
	switch m.AccessEnforce = strings.ToLower(def("HUB_ACCESS_ENFORCE", AccessOff)); m.AccessEnforce {
	case AccessOff, AccessWarn, AccessOn:
	default:
		bad("HUB_ACCESS_ENFORCE must be off, warn or on, got %q", m.AccessEnforce)
	}
	switch v := strings.ToLower(def("HUB_MACHINE_RESOURCES", "off")); v {
	case "on":
		m.Resources = true
	case "off":
	default:
		bad("HUB_MACHINE_RESOURCES must be on or off, got %q", v)
	}
	return m
}

// Values of HUB_PROVIDER_PROTOCOL.
const (
	ProviderProtocolV0 = "v0"
)
