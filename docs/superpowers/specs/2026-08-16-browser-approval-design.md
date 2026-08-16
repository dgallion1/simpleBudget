# Browser approval for guarded tools — design

The top rung of the consent ladder: the person answers on budget2's own page,
in the application they already trust, rather than in whatever UI is driving
the model. Extends `2026-08-15-elicitation-approval-design.md`, which built the
form rung and named this as the next slice.

## The ladder, and why it has three rungs

A guarded tool now climbs, best first:

1. **Browser** — the client is handed a link to `/mcp/approve/<id>`; the page
   shows the whole operation and two buttons.
2. **Form** — a boolean prompt rendered inside the model's client.
3. **Not at all** — the client can do neither; the confirm token alone
   authorizes the operation and the result says so.

Each rung is worse than the one above and the tool reports which it used.
The browser rung is the only one whose text is rendered by this application:
a client that auto-answers a form cannot be detected from here, but it cannot
click a button on a page it never opened.

## How URL elicitation actually behaves

The critical difference from form mode, and the thing that shapes everything
else: **the client's response does not carry the answer.** A URL elicitation
returns `accept` as soon as the client has shown the link — that is an
acknowledgement, not consent. The person's actual decision arrives later,
out of band, on the server's own page.

So the flow is:

1. Tool returns an `InputRequests` map holding a URL elicitation.
2. Client shows the link and immediately answers `accept`.
3. Client re-invokes the tool (multi-round-trip, SEP-2322).
4. Tool re-finds its own pending request and **blocks** until the person
   clicks, or the request expires.

Step 4 is why `Approvals.Await` exists and why the registry TTL doubles as the
tool call's timeout: two minutes, long enough to read a page and decide, short
enough that an unanswered call does not hang a session.

## The race that shapes the registry

Steps 2 and 4 are not ordered. The client returns instantly, so a person who
clicks quickly can answer **before** the tool comes back to wait. A registry
that deleted the request on `Decide` would drop that answer on the floor and
report a timeout to a user who said yes.

So a decided request outlives its decision: `Decide` marks it answered and
keeps it, `Find` returns answered requests, and `Await` returns immediately
when the answer is already there. `Get` — which the page uses — refuses
answered requests, so a back button cannot offer a second answer. The record
is dropped once `Await` consumes it, so an old approval can never be replayed
into a new operation.

Lookups from the tool are by `(tool, subject)`, never by an identifier
round-tripped through the client. Nothing the client sends is trusted:
`RequestState` is a breadcrumb for anyone reading the traffic.

## The page

`/mcp/approve/{id}`, registered **public**, alongside `/unlock` and `/killme`.
A guarded operation can need approving while storage is locked --
`shutdown_server` certainly can -- and the lock-check middleware would redirect
this page to the unlock screen exactly when it is needed. The capability is the
approval ID: 32 random bytes, single-use, expiring.

Everything that is not an explicit approval is a refusal: a missing form value,
an unexpected one, a closed tab. `html/template` escapes the detail text, which
matters because an archive name reaches that page.

Unknown, expired and already-answered requests all render the same "nothing to
approve" page. Telling them apart would only help someone guessing at IDs, and
the person who legitimately double-clicked needs the same sentence either way.

## A bug this work surfaced

`CanAsk` reported form support for any client declaring the elicitation
capability at all -- including one that declared **only** URL. Sending such a
client a form prompt fails the whole tool call rather than degrading. It now
mirrors the SDK's own rule: an explicit `Form` capability, or neither
sub-capability declared (which older clients leave empty and the SDK reads as
form support). Found by a test whose fake client advertised URL only.

## Known gap: no completion notification

When the person answers, the server should send
`notifications/elicitation/complete` so the client can drop its "finish this in
your browser" state. **go-sdk v1.7.0 exposes no public API for it** — the
method constant and the send path are both unexported, and v1.7.0 is the latest
release, so there is no version to upgrade to.

It is survivable in this flow rather than fatal: by the time the person clicks,
the client has already received its response and is waiting on the tool call
itself, which returns as soon as the answer lands. A client that renders a
separate pending-elicitation indicator may leave it up until then. Revisit when
the SDK exports a way to send notifications.

## Testing

- `confirm`: the whole registry, including both directions of the fast-click
  race, one-answer-per-request, expiry, context cancellation, and that a
  consumed answer cannot be replayed.
- `handlers/approval`: the page states the consequences and offers both
  answers; approving and declining each release a waiting tool; anything that
  is not `approve` is a refusal; unknown/answered/expired all read as gone; the
  detail text is escaped; a nil registry is harmless rather than a panic.
- `admin`: a real client advertising URL elicitation drives the real
  round-trip, with a fake "human" answering the way the page does. Approve,
  decline and nobody-answers are asserted against the data directory, and the
  URL is asserted to point at this server.
- Mutation-checked: ignoring the human's answer fails the decline and
  nobody-answers tests; deleting the record on `Decide` fails both fast-click
  tests.
