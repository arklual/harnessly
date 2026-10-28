package asks

// View is an ask as the page and the agent see it: without the chat's id, the sent HTML and the agent's session.
type View struct {
	ID           string   `json:"id"`
	Kind         Kind     `json:"kind"`
	Status       Status   `json:"status"`
	Question     string   `json:"question"`
	Details      string   `json:"details"`
	Link         string   `json:"link"`
	Options      []string `json:"options"`
	To           *Person  `json:"to"`
	Bot          string   `json:"bot"`
	Chat         string   `json:"chat"`
	Silent       bool     `json:"silent"`
	Conversation string   `json:"conversation"`
	Agent        string   `json:"agent"`
	CreatedAt    int64    `json:"createdAt"`
	Deadline     int64    `json:"deadline"`
	ClosedAt     int64    `json:"closedAt"`
	Decision     string   `json:"decision"`
	Answer       *Reply   `json:"answer"`
	Replies      []Reply  `json:"replies"`
	SeenAt       int64    `json:"seenAt"`
	SeenReplies  int      `json:"seenReplies"`
	Error        string   `json:"error"`
}

// View is the ask for the page and the agent.
func (a Ask) View() View {
	v := View{ID: a.ID, Kind: a.Kind, Status: a.Status, Question: a.Question, Details: a.Details, Link: a.Link,
		Options: a.Options, To: a.To, Bot: a.BotName, Chat: a.ChatTitle, Silent: a.Silent, Conversation: a.Conversation,
		Agent: a.Agent, CreatedAt: a.CreatedAt, Deadline: a.Deadline, ClosedAt: a.ClosedAt, Decision: a.Decision,
		Answer: a.Answer, Replies: a.Replies, SeenAt: a.SeenAt, SeenReplies: a.SeenReplies, Error: a.Error}
	if v.Options == nil {
		v.Options = []string{}
	}
	if v.Replies == nil {
		v.Replies = []Reply{}
	}
	return v
}

// Views are asks for the page.
func Views(asks []Ask) []View {
	out := make([]View, len(asks))
	for i, a := range asks {
		out[i] = a.View()
	}
	return out
}

// AgentContact is a contact as an agent sees it.
type AgentContact struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Note     string `json:"note,omitempty"`
	Approver bool   `json:"approver,omitempty"`
}

// AgentContacts are the contacts for `harnessly ask people`.
func (s Settings) AgentContacts() []AgentContact {
	out := make([]AgentContact, 0, len(s.Contacts))
	for _, c := range s.Contacts {
		out = append(out, AgentContact{Name: c.Name, Username: c.Username, Note: c.Note, Approver: c.CanApprove})
	}
	return out
}
