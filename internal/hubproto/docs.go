package hubproto

// Documents of a machine kept by the hub (docs/hub/docs.md). The agent sends what the closed cluster used to get: a
// publish (doc.publish) and an access change (doc.acl) with a request id the hub answers by the op doc.result; the
// conversation a document was published from (docs.bound); an agent's answer in a thread (docs.reply). The hub hands a
// person's comment to that conversation by docs.deliver.
//
// On the machine's own hub link this is always so. On an observe link only when the operator opted in with
// AGENT_HUB_DOCS=on: otherwise the cluster keeps the documents and the hub is told none of it.

const (
	// FeatureDocs is in `hello` when the hub keeps this machine's documents: the agent sends them to the hub and takes
	// docs.deliver. Without it the hub sends no comments to the machine.
	FeatureDocs = "docs"

	// EventDocPublish and EventDocACL carry a request id; the hub answers each with OpDocResult.
	EventDocPublish = "doc.publish"
	EventDocACL     = "doc.acl"
	// EventDocBound is {binding: DocBinding}: a document was published from a conversation of this machine.
	EventDocBound = "docs.bound"
	// EventDocReply is {threadId, body, resolve}: the agent answered in a thread of a document of this machine.
	EventDocReply = "docs.reply"

	// EventAutomationResult is {runId, resultToken, summary, bugIssueId?, mrUrl?}: a conversation the hub started for an
	// automation run reports what it did (docs/hub/automations.md).
	EventAutomationResult = "automations.result"
	// EventAutomationEvent is {source, fingerprint, data}: an event of a connector of this machine (docs/connectors.md),
	// the body POST /automations/hook takes with its event under data ("event" names the frame), for the rules of the
	// machine's owner only. The event carries no text of a letter or a message: the hub drops title, excerpt, text and
	// body if a machine sends them anyway.
	EventAutomationEvent = "automations.event"
	// OpConnectorsWebhook passes a request that came to a webhook's address on the hub (POST
	// <automations>/hook/m/<machine>/<connection>[/<secret>]) to the machine: {connection, path, header, body (base64)},
	// answered with {status} for the sender. The hub keeps neither the body nor the path's secret.
	OpConnectorsWebhook = "connectors.webhook"

	// OpDocResult is the hub's answer to a doc.publish or doc.acl: {requestId, status, body}.
	OpDocResult = "doc.result"
	// OpDocsDeliver hands a comment to the conversation a document is bound to: {tabId, sessionName, agentSessionId,
	// cwd, message}, answered with the runtime's {mode}.
	OpDocsDeliver = "docs.deliver"
)

// DocBinding is the conversation a document was published from. Same JSON as the agent sends in docs.bound.
type DocBinding struct {
	Slug           string  `json:"slug"`
	Kind           string  `json:"kind"`
	Key            *string `json:"key"`
	Title          string  `json:"title"`
	TabID          *string `json:"tabId"`
	SessionName    *string `json:"sessionName"`
	AgentSessionID *string `json:"agentSessionId"`
	Provider       string  `json:"provider"`
	SourcePath     *string `json:"sourcePath"`
	Cwd            *string `json:"cwd"`
}

// DocReply is an agent's answer in a thread.
type DocReply struct {
	ThreadID string `json:"threadId"`
	Body     string `json:"body"`
	Resolve  bool   `json:"resolve"`
}
