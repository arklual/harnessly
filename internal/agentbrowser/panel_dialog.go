package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
)

var errPanelDialogOpened = errors.New("panel input opened a dialog")

// Chrome may delay the reply to the input that opened a modal until the modal is
// answered. Stop waiting for that reply so the ordered panel queue can deliver
// the answer. The input has already reached Chrome; cancelling its local waiter
// does not undo it or change the order in which commands are dispatched.
func (i *Instance) panelCall(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	changes, unsubscribe := i.Changes()
	defer unsubscribe()
	if i.dialogError(id, "Runtime.evaluate") != nil {
		return i.Call(ctx, id, method, params)
	}
	waiting, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-waiting.Done():
				return
			case <-changes:
				if i.dialogError(id, "Runtime.evaluate") != nil {
					cancel(errPanelDialogOpened)
					return
				}
			}
		}
	}()
	defer func() { cancel(nil); <-done }()
	raw, err := i.Call(waiting, id, method, params)
	if errors.Is(context.Cause(waiting), errPanelDialogOpened) {
		return nil, nil
	}
	return raw, err
}
