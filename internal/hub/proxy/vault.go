package proxy

import (
	"regexp"

	"github.com/ly-solutions/harnessly/internal/hub/vaultsvc"
)

// The hub's central password safe (docs/hub/vault.md): the workspace's page asks it under the platform API,
//
//	<platform api>/vault/status | entries[/<id>[/fill|approve|deny|grants]] | audit
//
// and the hub answers itself, with the signed-in person's account, never forwarding to a machine.

var vaultPath = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/vault(?:/|$)`)

// UseVault turns the hub's safe on; nil leaves /vault to the machines, as before.
func (s *Service) UseVault(v *vaultsvc.Service) {
	s.mu.Lock()
	s.vault = v
	s.mu.Unlock()
}

func (s *Service) vaultService() *vaultsvc.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vault
}
