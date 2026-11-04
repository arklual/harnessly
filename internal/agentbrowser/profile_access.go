package agentbrowser

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ConversationIdentity is read from a local conversation record, not request arguments.
type ConversationIdentity struct {
	Project     string
	ProjectName string
	Title       string
}

// ProfileAccess coordinates permissions with live tabs. Future login and sealed-tab policy
// can use the same trusted conversation resolver without granting access by agent-supplied IDs.
type ProfileAccess struct {
	Store       *ProfileStore
	Manager     *Manager
	Asks        *ProfileAsks
	Identity    func(string) (ConversationIdentity, bool)
	Wait        time.Duration
	mu          sync.Mutex
	notices     map[string]string
	transitions sync.Map
}

// NewProfileAccess creates the shared authorization service for page, MCP and CLI callers.
func NewProfileAccess(store *ProfileStore, identity func(string) (ConversationIdentity, bool)) *ProfileAccess {
	return &ProfileAccess{Store: store, Asks: NewProfileAsks(), Identity: identity, Wait: 55 * time.Second, notices: map[string]string{}}
}

type conversationTransition struct {
	project string
	removed bool
}

func (a *ProfileAccess) identity(conv string) (ConversationIdentity, error) {
	if a.Identity == nil || conv == "" {
		return ConversationIdentity{}, errors.New("Cannot identify this chat")
	}
	identity, ok := a.Identity(conv)
	if !ok {
		return ConversationIdentity{}, errors.New("Cannot identify this chat")
	}
	// Actor events precede publication of the cached record. During that gap,
	// refuse access rather than recreating tabs with the previous project's rights.
	if value, ok := a.transitions.Load(conv); ok {
		transition := value.(conversationTransition)
		if transition.removed || transition.project != identity.Project {
			return ConversationIdentity{}, errors.New("Chat is changing; retry after the move or restore completes")
		}
	}
	return identity, nil
}

func (a *ProfileAccess) resolve(conv string) (Profile, ConversationIdentity, error) {
	identity, err := a.identity(conv)
	if err != nil {
		return Profile{}, identity, err
	}
	if previous, ok := a.Store.ProjectOf(conv); ok && previous != identity.Project {
		if err = a.reset(conv); err != nil {
			return Profile{}, identity, err
		}
	}
	p, err := a.Store.Resolve(conv, identity.Project, identity.ProjectName, identity.Title)
	return p, identity, err
}

// ProfileOf implements Config.ProfileOf. A resolver error never falls back to common.
func (a *ProfileAccess) ProfileOf(conv string) string {
	// Browser tasks invoke this callback while shutdown may be waiting for them.
	// Never acquire the access mutation lock or close tabs from this callback.
	// Conversation events perform cleanup; Store.Resolve still fails closed on a
	// changed project even if an event has not reached the page yet.
	identity, err := a.identity(conv)
	if err != nil {
		return "!unavailable"
	}
	p, err := a.Store.Resolve(conv, identity.Project, identity.ProjectName, identity.Title)
	if err != nil {
		return "!unavailable"
	}
	return p.ID
}

// List contains only the current conversation's permitted profiles and active selection.
func (a *ProfileAccess) List(conv string) ([]ProfileSummary, ProfileSummary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, identity, err := a.resolve(conv)
	if err != nil {
		return nil, ProfileSummary{}, err
	}
	return a.Store.Allowed(conv, identity.Project), summary(p), nil
}

// UseName resolves exclusively against the permitted set, so errors cannot reveal other profile names.
func (a *ProfileAccess) UseName(ctx context.Context, conv, name string) (ProfileSummary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, identity, err := a.resolve(conv)
	if err != nil {
		return ProfileSummary{}, err
	}
	for _, candidate := range a.Store.Allowed(conv, identity.Project) {
		if candidate.Name == name {
			if err = a.use(ctx, conv, identity.Project, p.ID, candidate.ID); err != nil {
				return ProfileSummary{}, err
			}
			return candidate, nil
		}
	}
	return ProfileSummary{}, ErrProfileUnavailable
}

// UseID is the page's equivalent of UseName; an ID is still checked against the chat's permissions.
func (a *ProfileAccess) UseID(ctx context.Context, conv, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, identity, err := a.resolve(conv)
	if err != nil {
		return err
	}
	return a.use(ctx, conv, identity.Project, p.ID, id)
}

func (a *ProfileAccess) use(ctx context.Context, conv, project, old, id string) error {
	permitted := false
	for _, p := range a.Store.Allowed(conv, project) {
		if p.ID == id {
			permitted = true
			break
		}
	}
	if !permitted {
		return ErrProfileUnavailable
	}
	if old == id {
		return nil
	}
	if a.Manager != nil {
		if err := a.Manager.CloseConversation(ctx, conv); err != nil {
			return err
		}
	}
	if err := a.Store.Select(conv, project, id); err != nil {
		return err
	}
	a.notice(conv)
	return nil
}

// Request waits on one card without holding the access mutex while the person decides.
func (a *ProfileAccess) Request(ctx context.Context, conv, why string) (ProfileAnswer, error) {
	a.mu.Lock()
	_, identity, err := a.resolve(conv)
	var ask ProfileAsk
	if err == nil {
		ask, err = a.Asks.Open(conv, identity.Project, identity.Title, why)
	}
	a.mu.Unlock()
	if err != nil {
		return ProfileAnswer{}, err
	}
	return a.Asks.Wait(ctx, ask.ID, a.Wait), nil
}

// Answer grants access only from the authenticated page, optionally creating a new profile.
func (a *ProfileAccess) Answer(ctx context.Context, askID, id, scope, name, color string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	ask, ok := a.Asks.Find(askID)
	if !ok {
		return errors.New("Profile request is no longer pending")
	}
	if scope == "decline" {
		a.Asks.Settle(askID, ProfileAnswer{Status: "declined"})
		return nil
	}
	if scope != "chat" && scope != "project" {
		return errors.New("Invalid access scope")
	}
	identity, err := a.identity(ask.Conv)
	if err != nil || identity.Project != ask.Project {
		a.Asks.Forget(ask.Conv)
		return errors.New("The requesting chat has moved or been removed")
	}
	if scope == "project" && identity.Project == "" {
		return errors.New("This chat has no project")
	}
	if id == "" {
		p, e := a.Store.Create(name, color)
		if e != nil {
			return e
		}
		id = p.ID
	}
	p, ok := a.Store.Lookup(id)
	if !ok {
		return ErrProfileUnavailable
	}
	if a.Manager != nil {
		if err = a.Manager.CloseConversation(ctx, ask.Conv); err != nil {
			return err
		}
	}
	if err = a.Store.Grant(ask.Conv, identity.Project, id, scope); err != nil {
		return err
	}
	a.notice(ask.Conv)
	a.Asks.Settle(askID, ProfileAnswer{Status: "ready", Name: summary(p).Name,
		Message: "Profile selected; this chat's tabs in the previous profile were closed."})
	return nil
}

func (a *ProfileAccess) reset(conv string) error {
	a.Asks.Forget(conv)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if a.Manager != nil {
		if err := a.Manager.CloseConversation(ctx, conv); err != nil {
			return err
		}
	}
	if err := a.Store.ForgetConversation(conv); err != nil {
		return err
	}
	a.notice(conv)
	return nil
}

// ConversationChanged handles record events without calling back into the conversation actor.
func (a *ProfileAccess) ConversationChanged(conv, project string, removed bool) error {
	a.transitions.Store(conv, conversationTransition{project: project, removed: removed})
	a.mu.Lock()
	defer a.mu.Unlock()
	old, seen := a.Store.ProjectOf(conv)
	if removed || seen && old != project {
		return a.reset(conv)
	}
	return nil
}

// ProjectAffectedChats counts chats whose existing tabs would close on applying access.
// It shares mutation validation but never resolves profiles, starts Chrome or writes metadata.
func (a *ProfileAccess) ProjectAffectedChats(project string, access ProjectAccess) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	convs, err := a.Store.projectChanges(project, access)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, conv := range convs {
		if a.Manager != nil && a.Manager.ConversationHasTabs(conv) {
			count++
		}
	}
	return count, nil
}

// SetProject closes affected tabs before applying the person's changed project permissions.
func (a *ProfileAccess) SetProject(ctx context.Context, project string, access ProjectAccess) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Extra grants and unchanged defaults must not discard working tabs.
	convs, err := a.Store.projectChanges(project, access)
	if err != nil {
		return err
	}
	for _, conv := range convs {
		if a.Manager != nil {
			if err := a.Manager.CloseConversation(ctx, conv); err != nil {
				return err
			}
		}
	}
	if err := a.Store.SetProject(project, access); err != nil {
		return err
	}
	for _, conv := range convs {
		a.notice(conv)
	}
	return nil
}

// Delete stops a managed profile before erasing data and every local permission referencing it.
func (a *ProfileAccess) Delete(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.Store.Lookup(id)
	if !ok {
		return ErrProfileUnavailable
	}
	a.Store.mu.Lock()
	affected := []string{}
	for conv, c := range a.Store.data.Conversations {
		selected := c.Selected
		if !allowed(&a.Store.data, conv, c.Project, selected) {
			selected = DefaultProfile
			if c.Project != "" {
				selected = a.Store.data.Projects[c.Project].Default
			}
		}
		if selected == id {
			affected = append(affected, conv)
		}
	}
	a.Store.mu.Unlock()
	remove := func() error { return a.Store.Remove(id) }
	var err error
	if a.Manager != nil {
		err = a.Manager.RemoveProfile(ctx, id, remove)
	} else {
		err = remove()
	}
	if err != nil {
		return err
	}
	for _, conv := range affected {
		a.notice(conv)
	}
	return nil
}

// ReconcileProjects drops bindings absent from an authoritative project snapshot.
// This also supports runtimes that remove projects without publishing a dedicated wire event.
func (a *ProfileAccess) ReconcileProjects(projects map[string]bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, bindings := a.Store.Snapshot()
	for project := range bindings {
		if !projects[project] {
			if err := a.Store.RemoveProject(project); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *ProfileAccess) notice(conv string) {
	a.notices[conv] = "Browser profile access changed; this chat's previous browser tabs were closed."
}

// TakeNotice reports page-initiated profile changes to the agent's next tool call.
func (a *ProfileAccess) TakeNotice(conv string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	notice := a.notices[conv]
	delete(a.notices, conv)
	return notice
}
