package agentbrowser

// LockConversation serializes complete tool actions and person control changes
// for one conversation. Callers must release the returned gate exactly once and
// must not acquire it again from inside an action that already holds it.
func (i *Instance) LockConversation(conv string) func() {
	i.mu.Lock()
	if i.conversationGates == nil {
		i.conversationGates = make(map[string]chan struct{})
	}
	gate := i.conversationGates[conv]
	if gate == nil {
		gate = make(chan struct{}, 1)
		i.conversationGates[conv] = gate
	}
	i.mu.Unlock()
	gate <- struct{}{}
	return func() { <-gate }
}
