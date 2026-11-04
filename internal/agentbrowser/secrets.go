package agentbrowser

import "github.com/ly-solutions/harnessly/internal/secrets"

// RememberSecret registers a value before input so metadata and registry writes cannot echo it.
// Values stay in memory only, including after navigation; sibling tabs can receive page messages.
func (i *Instance) RememberSecret(value string) {
	if value == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, old := range i.secretMasks {
		if old == value {
			return
		}
	}
	i.secretMasks = append(i.secretMasks, value)
	i.changedLocked()
}

func (i *Instance) redactLocked(s string) string {
	return secrets.Mask(s, i.secretMasks)
}

// Redact removes registered secret bytes from a tool response.
func (i *Instance) Redact(s string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.redactLocked(s)
}

// Seal persists only a document marker, never the value. A runtime restart fails closed
// until the main frame reaches another document because node references were in memory.
func (i *Instance) Seal(conv, id, document string) error {
	if err := i.agentAllowed(conv, id, ""); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil {
		return ErrNotOwner
	}
	p.Sealed, p.SecretDocument = true, document
	p.SecretTouched = true
	i.changedLocked()
	return nil
}

// Unseal removes the read restriction after a browser-verified document or field change.
func (i *Instance) Unseal(conv, id string) error {
	if err := i.agentAllowed(conv, id, ""); err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil {
		return ErrNotOwner
	}
	p.Sealed, p.SecretDocument = false, ""
	i.changedLocked()
	return nil
}
