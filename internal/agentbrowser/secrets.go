package agentbrowser

import (
	"errors"
	"slices"
	"sync"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

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
	values := append([]string(nil), i.secretMasks...)
	for _, pending := range i.maskPending {
		values = append(values, pending...)
	}
	return secrets.Mask(s, values)
}

// Redact removes registered secret bytes from a tool response.
func (i *Instance) Redact(s string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.redactLocked(s)
}

// Seal persists only a document marker, never the value. A runtime restart fails closed
// until the main frame reaches another document because node references were in memory.
// It also protects uncertain input if the person took control during the CDP call.
func (i *Instance) Seal(conv, id, document string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil || p.Conversation != conv {
		return ErrNotOwner
	}
	p.SealGeneration++
	p.Sealed, p.SecretDocument = true, document
	p.SecretTouched = true
	i.changedLocked()
	return nil
}

// Unseal removes only the seal whose browser proof was checked by the caller.
// Callers hold LockConversation across that proof, the reset and subsequent tool dispatch.
func (i *Instance) Unseal(conv, id string, generation uint64) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil || p.Conversation != conv {
		return ErrNotOwner
	}
	if p.Control == "person" {
		return ErrPersonControl
	}
	if !p.Sealed || p.SealGeneration != generation {
		return errors.New("browser seal changed during verification")
	}
	p.SealGeneration++
	p.Sealed, p.SecretDocument, p.SecretUntracked = false, "", false
	i.changedLocked()
	return nil
}

// ReserveSecrets masks an in-flight input before any page event can echo it.
// Finish commits successful or uncertain input, or removes a reservation that never reached the page.
func (i *Instance) ReserveSecrets(values ...string) func(bool) {
	i.mu.Lock()
	i.maskNext++
	id := i.maskNext
	if i.maskPending == nil {
		i.maskPending = map[uint64][]string{}
	}
	i.maskPending[id] = append([]string(nil), values...)
	i.mu.Unlock()
	var once sync.Once
	return func(keep bool) {
		once.Do(func() {
			i.mu.Lock()
			defer i.mu.Unlock()
			if keep {
				for _, value := range i.maskPending[id] {
					if value != "" && !slices.Contains(i.secretMasks, value) {
						i.secretMasks = append(i.secretMasks, value)
					}
				}
			}
			delete(i.maskPending, id)
			i.changedLocked()
		})
	}
}

// BeginSecretInput persists a provisional seal before the renderer can receive a value.
// The control gate keeps a rejected attempt from rolling back a concurrent person's handoff.
// Finish restores the prior state only when insertion is known not to have happened.
func (i *Instance) BeginSecretInput(conv, id, document string) (func(bool), error) {
	i.mu.Lock()
	p := i.pages[id]
	i.mu.Unlock()
	if p == nil {
		return nil, ErrNotOwner
	}
	p.controlMu.Lock()
	if err := i.agentAllowed(conv, id, ""); err != nil {
		p.controlMu.Unlock()
		return nil, err
	}
	i.mu.Lock()
	previous := p.Tab
	p.SealGeneration++
	p.Sealed, p.SecretTouched, p.SecretDocument = true, true, document
	i.changedLocked()
	i.mu.Unlock()
	var once sync.Once
	finish := func(keep bool) {
		once.Do(func() {
			if !keep {
				i.mu.Lock()
				if i.pages[id] == p {
					p.SealGeneration++
					p.Sealed, p.SecretTouched, p.SecretDocument = previous.Sealed, previous.SecretTouched, previous.SecretDocument
					p.SecretUntracked = previous.SecretUntracked
					i.changedLocked()
				}
				i.mu.Unlock()
			}
			p.controlMu.Unlock()
		})
	}
	i.save()
	i.mu.Lock()
	err := i.saveError
	i.mu.Unlock()
	if err != nil {
		finish(false)
		return nil, err
	}
	return finish, nil
}
