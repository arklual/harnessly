package delegation

import (
	"context"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/chatrole"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// Roles of the chats (internal/chatrole): a delegated chat started with plan_first plans without changing anything until
// its parent approves the plan; a chat the person made a coordinator hands work out and does none itself
// The book keeps each chat's role; the runtime marks the chat's agent so its PreToolUse hook refuses what
// the role does not allow (RoleMarker).

// Plan states of a delegated chat started with plan_first.
const (
	PlanDrafting  = "drafting"
	PlanSubmitted = "submitted"
	PlanApproved  = "approved"
)

// RoleMarker marks a chat's agent with its role. The sweep calls it for every chat with a role every few seconds, so it
// writes only what changed and marks a chat whose agent starts (or moved to another session) when it can.
type RoleMarker func(conv string, r chatrole.Role)

// PlanRules are added to the task of a chat that must show its plan first.
const PlanRules = "\n- This task starts with a plan: until your parent approves it you may read and look around but not change " +
	"files or run commands that write (they are refused). Work out the plan, send it with submit_plan, then end the turn " +
	"with " + MarkQuestion + ". You are told when it is approved (go on with the work then) or what to change in it."

// applyRoles marks every chat of the book with its role: what the sweep runs.
func (s *Service) applyRoles() {
	if s.Roles == nil {
		return
	}
	for id, r := range s.Book.roles() {
		s.Roles(id, r)
	}
}

func (s *Service) setRole(conv string, r chatrole.Role) {
	s.Book.Update(conv, func(p *Page) { p.Role = string(r) })
	if s.Roles != nil {
		s.Roles(conv, r)
	}
}

// RoleOf is a chat's role as the book has it.
func (s *Service) RoleOf(conv string) chatrole.Role {
	p, _ := s.Book.Get(conv)
	return chatrole.Role(p.Role)
}

// SubmitPlan sends the caller's plan to its parent for approval.
func (s *Service) SubmitPlan(ctx context.Context, callerID, plan string) error {
	caller, err := s.caller(callerID)
	if err != nil {
		return err
	}
	page, _ := s.Book.Get(caller.ID)
	if caller.ParentID == nil || page.Plan == "" {
		return refuse("not-allowed", "this chat was not asked for a plan first; go on with the work")
	}
	if page.Plan == PlanApproved {
		return refuse("not-allowed", "your plan is approved already; go on with the work")
	}
	plan = strings.TrimSpace(plan)
	if plan == "" || len([]rune(plan)) > PromptMax {
		return refuse("bad-request", "plan: 1 to %d characters", PromptMax)
	}
	parent, ok := s.Convs.Record(*caller.ParentID)
	if !ok {
		return refuse("not-found", "the parent chat is gone")
	}
	text := fmt.Sprintf("Chat %s «%s» asks you to approve its plan before it changes anything:\n\n%s\n\n"+
		"Approve it with approve_plan (id %s, approve: true), or send it back with approve: false and feedback on what to change.",
		caller.ID, caller.Title, plan, caller.ID)
	if _, res := s.deliver(ctx, caller, parent.ID, text, deliverStep); res.R == wire.ResultRefused {
		return refuse(string(res.Code), "the plan did not reach the parent")
	}
	s.Book.Update(caller.ID, func(p *Page) { p.Plan = PlanSubmitted })
	return nil
}

// ApprovePlan approves a delegated chat's plan (its edits are allowed from now on) or sends it back with feedback.
func (s *Service) ApprovePlan(ctx context.Context, callerID, id string, approve bool, feedback string) error {
	caller, err := s.caller(callerID)
	if err != nil {
		return err
	}
	rec, err := s.target(caller, id)
	if err != nil {
		return err
	}
	page, _ := s.Book.Get(rec.ID)
	switch page.Plan {
	case "":
		return refuse("not-allowed", "chat %s was not started with plan_first", id)
	case PlanApproved:
		return refuse("not-allowed", "the plan of chat %s is approved already", id)
	}
	feedback = strings.TrimSpace(feedback)
	var text string
	if approve {
		s.Book.Update(rec.ID, func(p *Page) { p.Plan = PlanApproved })
		s.setRole(rec.ID, chatrole.None)
		text = "Your plan is approved: changes are allowed now. Carry it out."
		if feedback != "" {
			text += "\n\nNotes:\n" + feedback
		}
	} else {
		if feedback == "" {
			return refuse("bad-request", "say in feedback what to change in the plan")
		}
		s.Book.Update(rec.ID, func(p *Page) { p.Plan = PlanDrafting })
		text = "Your plan is not approved yet. Change it and send it again with submit_plan:\n\n" + feedback
	}
	_, err = s.Send(ctx, callerID, rec.ID, text, DeliverDefault)
	return err
}

// Coordinate makes the caller's chat a coordinator, or lets it work again.
func (s *Service) Coordinate(callerID string, on bool) error {
	caller, err := s.caller(callerID)
	if err != nil {
		return err
	}
	if s.RoleOf(caller.ID) == chatrole.Plan {
		return refuse("not-allowed", "this chat waits for its plan to be approved; send it with submit_plan")
	}
	if on {
		s.setRole(caller.ID, chatrole.Coordinate)
	} else if s.RoleOf(caller.ID) == chatrole.Coordinate {
		s.setRole(caller.ID, chatrole.None)
	}
	return nil
}
