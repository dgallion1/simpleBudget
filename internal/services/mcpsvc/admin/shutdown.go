package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"budget2/internal/services/mcpsvc/confirm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// shutdownExitDelay is how long the tool waits before stopping the process,
// so the JSON-RPC response is written first. A handler that exits inline
// never delivers its result. Mirrors handlers/backup's /killme, which sleeps
// 100ms for the same reason.
const shutdownExitDelay = 250 * time.Millisecond

type shutdownInput struct {
	ConfirmToken string `json:"confirm_token,omitempty" jsonschema:"the token returned by a previous call; omit it to get the preview and a fresh token"`
}

type shutdownOutput struct {
	Confirmed       bool   `json:"confirmed"`
	ConfirmToken    string `json:"confirm_token,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	WhatWouldHappen string `json:"what_would_happen,omitempty"`

	// HumanApproval reports whether a person actually agreed; see
	// restoreBackupOutput for why it is a string rather than a bool.
	HumanApproval string `json:"human_approval,omitempty"`

	Note string `json:"note,omitempty"`
}

const shutdownConsequences = "the budget2 server process stops; every MCP tool in this session stops answering, " +
	"any open browser tab stops working, and NOTHING in this session can start it again -- the user must " +
	"restart the server themselves, and then restart this session, because the tools are only registered if " +
	"the server was already running when the session began"

func registerShutdown(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "shutdown_server",
		Description: "Stop the budget2 server. THIS IS NOT RECOVERABLE FROM INSIDE THIS SESSION: after it runs, " +
			"every tool here stops working and nothing in this session can bring the server back -- the user " +
			"must restart it themselves and then start a new session, because these tools are only registered " +
			"if the server was already running when the session began. Do not call this to 'restart' anything; " +
			"there is no restart. Two steps: call it with no arguments to get a description of what would happen " +
			"plus a confirm_token, show that to the user, and only call again with the token if they say yes. " +
			"The token is single-use, bound to this tool, and expires; a wrong or reused one is refused and you " +
			"must start over. Confirming twice yourself is not the user agreeing -- the second call is for after " +
			"they have actually answered.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in shutdownInput) (res *mcp.CallToolResult, out shutdownOutput, err error) {
		defer recoverToError("shutdown_server", &err)

		if deps.Shutdown == nil {
			return nil, shutdownOutput{}, fmt.Errorf("no shutdown path is configured on this server")
		}
		if deps.Confirm == nil {
			return nil, shutdownOutput{}, fmt.Errorf("no confirmation registry is configured on this server, so this guarded tool cannot run")
		}

		token := strings.TrimSpace(in.ConfirmToken)
		if token == "" {
			fresh, expires, mintErr := deps.Confirm.Mint("shutdown_server", shutdownInput{})
			if mintErr != nil {
				return nil, shutdownOutput{}, mintErr
			}
			return nil, shutdownOutput{
				Confirmed:       false,
				ConfirmToken:    fresh,
				ExpiresAt:       expires.UTC().Format(time.RFC3339),
				WhatWouldHappen: shutdownConsequences,
				Note:            "nothing has been shut down; show the user what_would_happen and call again with confirm_token ONLY if they agree",
			}, nil
		}

		// The minted args are the zero input, not `in` -- `in` carries the
		// token itself, so hashing it would never match what Mint recorded.
		//
		// Same shape as restore_backup: ask a real person before spending the
		// token, via a multi-round-trip request that re-invokes this handler
		// with their answer. Hence Check now, Redeem on the way back.
		approval := confirm.NotAsked
		answer, answered := req.Params.InputResponses[confirm.ApprovalID]
		if !answered {
			if err := deps.Confirm.Check(token, "shutdown_server", shutdownInput{}); err != nil {
				return nil, shutdownOutput{}, err
			}
			if confirm.CanAsk(req.Session) {
				return &mcp.CallToolResult{
					InputRequests: mcp.InputRequestMap{
						confirm.ApprovalID: confirm.ApprovalRequest("stop the budget2 server", shutdownConsequences),
					},
					RequestState: "shutdown_server",
				}, shutdownOutput{}, nil
			}
		} else if approval = confirm.DecisionFrom(answer); approval != confirm.Approved {
			// Left unspent: nothing happened, and the token costs nothing to
			// hold until it expires.
			return nil, shutdownOutput{
				Confirmed:     false,
				HumanApproval: confirm.Refused.String(),
				Note: "the user was asked and did NOT approve, so the server is still running. " +
					"Do not retry this without being told to",
			}, nil
		}

		if err := deps.Confirm.Redeem(token, "shutdown_server", shutdownInput{}); err != nil {
			return nil, shutdownOutput{}, err
		}

		// Return first, exit after. Reversing these loses the response.
		shutdown := deps.Shutdown
		time.AfterFunc(shutdownExitDelay, shutdown)

		out = shutdownOutput{
			Confirmed:     true,
			HumanApproval: approval.String(),
			Note:          "the server is shutting down; this is the last answer any tool in this session will give",
		}
		if approval == confirm.NotAsked {
			out.Note += ". NO HUMAN WAS ASKED: this client cannot prompt anyone, so the confirmation token alone " +
				"authorized this"
		}
		return nil, out, nil
	})
}
