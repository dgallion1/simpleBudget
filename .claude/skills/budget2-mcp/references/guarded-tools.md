# Guarded tools — the two-call protocol and the approval ladder

Four tools are guarded: `restore_backup`, `shutdown_server`,
`resolve_transfer`, `set_balance_anchor`. Each can do something a `.bak`
file cannot fix — erase data, kill the session, or silently corrupt every
total — so each takes two calls and, where the client allows it, puts the
question to an actual human.

## The two-call protocol

1. **First call — no token.** Pass the operation's real arguments but omit
   `confirm_token`. Nothing happens; you get back `what_would_happen` (a
   plain-language preview) and a fresh `confirm_token` with an expiry.
2. **Show the preview to the user and wait for their answer.** This is the
   point of the design. The token exists to force this pause.
3. **Second call — only after the user says yes.** Same arguments, plus the
   token. The operation runs, and on a capable client the call *also* puts
   the question to the human directly before acting.

Token rules, all enforced server-side: single-use, expires (minutes), bound
to the tool **and to the first call's arguments** — change the archive name,
the pair key, the verdict, or the anchor values and the token is refused.
A wrong, reused, or expired token means start over from the first call.

**Confirming twice yourself is not the user agreeing.** The second call is
for after they have actually answered. Calling twice in one turn defeats the
entire mechanism and is the specific failure the design documents call out.

## The approval ladder

On the second call, the server tries to reach a human by the strongest rung
the client supports:

1. **Browser.** The client is handed a link to the server's own
   `/mcp/approve/<id>` page. The user reads the whole operation rendered by
   budget2 itself — not by the client — and clicks approve or decline.
   Requests expire after two minutes; closing the tab is a decline. This is
   the strongest rung because a client that auto-answers prompts cannot
   click a button on a page it never opened. Your job: surface the URL to
   the user clearly and wait — the tool call blocks until they answer or it
   expires.
2. **In-client form prompt.** A yes/no question rendered by whatever is
   driving the model, carrying the same consequences text. Only an explicit
   yes is a yes: a decline, a dismissal, or an empty accept are all
   refusals, and a refusal leaves the token unspent.
3. **Token alone.** The client can do neither. The operation proceeds on the
   token, and the result says so.

## Reading `human_approval`

Every guarded result reports `human_approval` as a string — deliberately not
a bool, so an unasked operation cannot be skimmed as an approved one:

- **`approved`** — a person said yes. Report the outcome normally.
- **`refused`** — a person said no. **Do not retry**, do not re-mint a
  token, do not rephrase and ask again. Tell the user it was refused and
  stop.
- **`not asked`** — this client cannot prompt anybody, so the token alone
  authorized the operation. Nobody outside the model sanctioned it: say
  plainly what was done and that no human approved it, so the user can check
  the result.

## Per-tool stakes (why each one earned the guard)

- `restore_backup` — deletes every file the archive lacks; recovery only via
  the safety snapshot it takes first; no undo tool.
- `shutdown_server` — every tool stops answering; nothing in the session can
  restart the server; there is no restart semantics at all.
- `resolve_transfer` — a wrong `confirm` reclassifies real income or real
  spending as a transfer, silently removing it from every total.
- `set_balance_anchor` — every balance and projection rolls forward from
  anchors; a wrong amount makes the dashboard lie about the user's money,
  and a same-day anchor overwrites the existing one.
