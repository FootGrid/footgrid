# First implementation plan

## Goal

Ship one reliable vertical slice: an organizer creates a 6v6 match, sets
rosters/lineups, starts the match, logs a goal or substitution, reverses a
mistake, and another device receives the same authoritative snapshot.

## Delivery order

1. Apply migrations and seed the action catalog.
2. Implement identity organization/membership authorization.
3. Implement match setup: `POST /matches`, roster, initial lineup, and READY.
4. Implement live session, append-only event writes and reversals.
5. Add outbox publishing and snapshot/timeline/stat projections.
6. Add WebSocket notification fan-out; HTTP remains authoritative for replay.
7. Enable Cognito verification, WAF/rate limits, alarms and deployment CI.

## Non-negotiable rules

* Events are immutable. Undo inserts a reversal; it does not delete an event.
* Every write has `Idempotency-Key`; event appends also carry a client UUID and
  an expected match event sequence.
* Scores and stats are derived from the ledger, never independently edited.
* Anonymous opposition is represented by a match-scoped participant with a
  shirt number, not a fake user account.
* Projection consumers use an inbox table and may rebuild from the ledger.

## Definition of done for the first release

* Offline retries cannot duplicate a score-changing event.
* Two scorers racing to append at the same sequence return one success and one
  `409` conflict with enough data for the client to rebase.
* A finalized match cannot be modified by ordinary scorers.
* Event-to-public-score latency is below two seconds in the primary region.
