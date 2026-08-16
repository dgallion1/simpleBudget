package confirm

import (
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ApprovalID is the key a guarded tool files its approval request under, and
// the key the answer comes back under. One constant so the two halves of a
// multi-round-trip cannot disagree.
const ApprovalID = "approval"

// Decision is what a human said about a guarded operation.
//
// It is deliberately three-valued. "Not asked" is not "no" -- refusing every
// operation on a client that cannot prompt would make the guarded tools
// useless everywhere -- and it is emphatically not "yes", which is the whole
// point of tracking it separately: the tool reports which one happened rather
// than letting an unasked operation read as an approved one.
type Decision int

const (
	// NotAsked means no human saw this. The client did not declare the
	// elicitation capability, so there was nobody to ask.
	NotAsked Decision = iota
	// Approved means a human was shown the consequences and agreed.
	Approved
	// Refused means a human was asked and did not agree.
	Refused
)

func (d Decision) String() string {
	switch d {
	case Approved:
		return "approved"
	case Refused:
		return "refused"
	default:
		return "not asked"
	}
}

// confirmField is the one thing an approval form asks for. A bare accept
// would be satisfied by a client that auto-accepts empty forms; requiring an
// explicit true means the approval had to be entered, not merely not-refused.
const confirmField = "confirm"

// CanAsk reports whether the client on this session can put a question to a
// human, i.e. whether it declared the elicitation capability at initialize.
//
// Guarded tools MUST check this before returning an approval request. The
// SDK's compatibility middleware fulfills input requests by calling the
// client directly, and on a client without elicitation that call fails the
// whole tool call -- so returning a request unconditionally would turn "this
// client cannot prompt" into "this tool never works here" instead of the
// documented fallback.
func CanAsk(ss *mcp.ServerSession) bool {
	if ss == nil {
		return false
	}
	p := ss.InitializeParams()
	if p == nil || p.Capabilities == nil || p.Capabilities.Elicitation == nil {
		return false
	}
	// Mirrors the SDK's own rule for form elicitation: an explicit Form
	// capability, or NEITHER sub-capability declared, which older clients
	// leave empty and which the SDK reads as form support. A client that
	// declared only URL cannot render a form, and asking it to would fail the
	// call rather than degrade -- which is exactly the mistake this function
	// exists to prevent.
	e := p.Capabilities.Elicitation
	return e.Form != nil || e.URL == nil
}

// ApprovalRequest builds the question a human answers before a guarded
// operation runs. message must already describe the consequences in full --
// it is the only thing the person sees, and nothing downstream summarizes it.
// action is a short verb phrase ("restore this backup") for the form field.
//
// This is the thing a confirmation token cannot do. A token proves the caller
// called twice; a model can mint and redeem one inside a single turn without
// a human ever seeing the preview. This request goes to the client, which
// shows it to the person using it, and the tool call does not proceed until
// they answer.
func ApprovalRequest(action, message string) *mcp.ElicitParams {
	return &mcp.ElicitParams{
		Mode:    "form",
		Message: message,
		RequestedSchema: &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				confirmField: {
					Type:        "boolean",
					Description: "Set true ONLY if you want to " + action + ". This cannot be undone from this session.",
				},
			},
			Required: []string{confirmField},
		},
	}
}

// DecisionFrom interprets the answer to an ApprovalRequest.
//
// Everything that is not an explicit, affirmative accept is Refused: a
// decline, a dismissal, a missing answer, and an accept whose form says false
// (a person who read the prompt and left the box unticked). A destructive
// operation needs a yes, and only a yes is a yes.
func DecisionFrom(resp mcp.InputResponse) Decision {
	res, ok := resp.(*mcp.ElicitResult)
	if !ok || res == nil {
		return Refused
	}
	if res.Action != "accept" {
		return Refused
	}
	v, ok := res.Content[confirmField].(bool)
	if !ok || !v {
		return Refused
	}
	return Approved
}

// CanAskInBrowser reports whether the client can hand a human a URL to open
// (the "url" elicitation sub-capability).
//
// Preferred over CanAsk when both are available: a browser page can show the
// operation in full, in the application the person already trusts, instead of
// a boolean rendered by whatever UI is driving the model.
func CanAskInBrowser(ss *mcp.ServerSession) bool {
	if ss == nil {
		return false
	}
	p := ss.InitializeParams()
	return p != nil && p.Capabilities != nil &&
		p.Capabilities.Elicitation != nil && p.Capabilities.Elicitation.URL != nil
}

// BrowserApprovalRequest points the client at the app's own approval page for
// this pending request.
//
// KNOWN GAP: when the person answers, the server should send
// notifications/elicitation/complete so the client can stop showing its
// "finish this in your browser" state. go-sdk v1.7.0 exposes no public API for
// that notification -- the constant and the send path are unexported -- so it
// is not sent. In the multi-round-trip flow this is survivable: the client has
// already received its response and is waiting on the tool call itself, which
// returns when the human answers. A client that renders a separate pending-
// elicitation indicator may leave it up until then.
func BrowserApprovalRequest(p *Pending, baseURL, message string) *mcp.ElicitParams {
	return &mcp.ElicitParams{
		Mode:          "url",
		Message:       message,
		URL:           strings.TrimSuffix(baseURL, "/") + "/mcp/approve/" + p.ID,
		ElicitationID: p.ID,
	}
}
