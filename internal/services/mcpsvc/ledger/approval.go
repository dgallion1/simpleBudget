package ledger

import (
	"context"
	"errors"
	"fmt"

	"budget2/internal/services/mcpsvc/confirm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The consent ladder, best first. Both guarded ledger tools climb it
// identically, mirroring admin/approval.go so a reader never has to work out
// which tool asks how:
//
//  1. Browser  -- the person answers on the app's own page, having read the
//     whole operation. The client only carries the URL.
//  2. Form     -- the person answers a boolean inside the model's client.
//  3. Not at all -- the client cannot prompt anybody; the confirm token alone
//     authorizes the operation and the answer says so out loud.

// askForApproval files an approval request and returns the input request that
// sends the client to it, or false when this session cannot ask in a browser.
// opID identifies the exact operation being asked about -- in practice the
// confirm token, already bound to the arguments shown to the human -- so a
// different operation on the same tool and subject (a different amount, a
// different verdict) never replaces this one.
func askForApproval(deps Deps, req *mcp.CallToolRequest, tool, subject, opID, title, detail string) (*mcp.CallToolResult, bool) {
	if deps.Approvals == nil || deps.BaseURL == "" || !confirm.CanAskInBrowser(req.Session) {
		return nil, false
	}
	pending, err := deps.Approvals.Create(tool, subject, opID, title, detail)
	if err != nil {
		// Could not open a request, so there is nothing to send anyone to.
		// Fall through to the next rung rather than failing the call.
		return nil, false
	}
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{
			confirm.ApprovalID: confirm.BrowserApprovalRequest(pending, deps.BaseURL,
				title+" -- open this to read what would happen and answer. Nothing happens until you do."),
		},
		// A breadcrumb for anyone reading the traffic. Not trusted: the
		// pending request is re-found server-side by tool, subject and opID.
		RequestState: tool + ":" + subject,
	}, true
}

// awaitApproval returns the human's answer to an outstanding browser request,
// blocking until they answer it or it expires. The third result is false when
// there was no browser request outstanding, which means this session used a
// different rung of the ladder. opID must be the same operation identity
// passed to askForApproval, so this only ever waits on the request it itself
// filed, never on some other operation that happens to share tool and
// subject.
func awaitApproval(ctx context.Context, deps Deps, tool, subject, opID string) (confirm.Decision, error, bool) {
	if deps.Approvals == nil {
		return confirm.NotAsked, nil, false
	}
	pending, ok := deps.Approvals.Find(tool, subject, opID)
	if !ok {
		return confirm.NotAsked, nil, false
	}
	d, err := deps.Approvals.Await(ctx, pending)
	return d, err, true
}

// approvalRefusal turns a non-approval into the sentence the model should
// relay. A timeout and a refusal are different facts about the user and must
// not be collapsed: one says they declined, the other says they never saw it.
func approvalRefusal(tool string, err error) string {
	switch {
	case errors.Is(err, confirm.ErrApprovalTimeout):
		return fmt.Sprintf("nobody answered the approval page in time, so %s did nothing. "+
			"The user may not have seen it -- tell them it is waiting and offer to ask again", tool)
	case err != nil:
		return fmt.Sprintf("the approval was not completed (%v), so %s did nothing", err, tool)
	default:
		return fmt.Sprintf("the user opened the approval page and said NO, so %s did nothing. "+
			"Do not retry this without being told to; ask them what they want instead", tool)
	}
}
