# Human approval for guarded tools — design

Closes the gap the restore-backup slice accepted rather than solved: a confirm
token proves the caller called twice, not that a human agreed. Guarded tools
now put the operation to an actual person through MCP elicitation, and report
which of the three things happened.

Supersedes the "the third objection stands and was accepted" paragraph of
`2026-08-15-restore-backup-design.md`.

## What the protocol actually allows

The obvious implementation — call `ServerSession.Elicit` from inside the tool
handler and block on the answer — **does not work on protocol version
2026-07-28**. The SDK refuses it:

```
"elicitation/create" cannot be sent while serving a request on protocol
version 2026-07-28: return an InputRequests map instead
(multi round-trip requests, SEP-2322)
```

The supported shape is a multi-round-trip request. The handler returns a
`CallToolResult` carrying `InputRequests`; the client fulfills them and
**re-invokes the same tool call** with `InputResponses` set. For clients on
older protocol versions the SDK's own server-side middleware bridges this by
performing the classic server-initiated elicitation and reinvoking the handler
once, so one handler shape serves both generations.

Two consequences fall out of that and both are load-bearing:

1. **A guarded handler runs twice per confirmation.** Anything it consumes on
   the first pass is gone by the second.
2. **The answer arrives as a normal tool-call argument**, so the handler must
   re-validate everything on the retry rather than trusting that the first
   pass checked it.

## Check, then ask, then redeem

The confirmation token is single-use, so a handler that redeemed it before
returning an approval request would find it already spent when the client
retried. The registry therefore grows `Check`, which validates exactly what
`Redeem` validates and consumes nothing. The order is: `Check` → return the
approval request → (client asks the human) → `Redeem` → act.

`Redeem` remains the only path that spends a token, so single-use is still
single-use. `Check` does not burn a token presented with the wrong tool or
arguments the way `Redeem` deliberately does — there is nothing to burn when
nothing is consumed.

`RequestState` is set on the outbound request and echoed back by the client. It
is a breadcrumb for anyone reading the traffic, **not** a fact the handler
trusts: every check re-runs on the retry against the tool arguments, so forging
it buys nothing.

## Fail open, loudly

`CanAsk` reports whether the client declared the elicitation capability. A
guarded tool must check it before returning an approval request, because the
compatibility middleware fulfills input requests by calling the client — and on
a client with no elicitation handler that call fails the whole tool call.
Returning the request unconditionally would turn "this client cannot prompt"
into "this tool never works here".

So when nobody can be asked, the operation proceeds on the token alone. That is
a policy choice, and the alternative — refusing every guarded operation on
every client without elicitation, which today is most of them — was rejected as
making the tools useless. What makes it honest is that the result says so:
`human_approval` is `approved`, `refused`, or `not asked`, and the note on a
`not asked` run states in plain words that no human sanctioned the write. It is
a string, not a bool, precisely so an unasked operation cannot be skimmed as an
approved one.

## Only a yes is a yes

`DecisionFrom` returns `Approved` **only** for an accept whose form carries
`confirm: true`. A decline, a dismissal, a missing answer, a response of the
wrong type, and an accept with an empty form are all `Refused`.

The required boolean exists because a bare accept is satisfied by a client that
auto-accepts empty forms; requiring the field means the approval had to be
entered rather than merely not-refused. A refusal leaves the token unspent —
nothing happened, and the user may well approve a different archive a moment
later.

## Applied to both guarded tools

`restore_backup` and `shutdown_server` get the same treatment. Two guarded
tools with different consent semantics would be worse than either choice made
consistently: a reader could not tell which one asks.

## Behavior change worth noting

`restore_backup`'s confirm call now resolves the archive **before** spending
anything, because the approval prompt has to quote the real archive. An archive
pruned between the preview and the confirmation is therefore refused early,
with the data untouched and the token unspent, instead of failing after the
redeem. The old test asserting "the token has been spent" was rewritten: that
sentence is no longer true, and asserting it would have been asserting a worse
behavior.

## What this still does not do

- **URL-mode elicitation.** The SDK supports it, and pointing a client at
  budget2's own Backup page would give a far better prompt than a boolean
  form. It needs a route, a pending-approval registry and an
  `elicitation/complete` notification, which is its own slice.
- **Anything about clients that lie.** A client that auto-fills `confirm: true`
  reports approval that never happened. Nothing server-side can detect that;
  the browser page is the path that does not depend on the client's honesty.

## Testing

- `confirm`: `CanAsk` against real initialized sessions with and without an
  elicitation handler; every not-a-yes shape; `Check` validating without
  consuming, including that a redeemed token stops checking.
- `admin`: a real client with an elicitation handler drives the actual
  round-trip for approve, decline and empty-accept, asserting on the data
  directory rather than the flags — a refused restore must leave the file it
  would have pruned. The prompt text itself is asserted per tool, since a
  prompt that does not state the consequence is theater.
- Mutation-checked: never asking, treating a bare accept as approval, and
  redeeming before asking each fail their tests.
