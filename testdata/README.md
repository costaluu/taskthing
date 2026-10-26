# testdata

Reference data the TypeScript implementation of taskthing generated, frozen at
the Go port. The Go code must keep reproducing it:

- `mdwal/` — a workspace's files and `events.log` byte for byte, and the
  result of replaying two peers' logs under LWW (a tie keeps the incumbent, so
  replay order matters).
- `config/` — `config.md` as written fresh and after an update check.
- `corpus/` — what chrono-node and rrule.js answered: natural-language dates
  (`dates.json`, per time zone), the `add` line parser (`add-input.json`),
  recurrence stepping/moving/phrasing (`recurrence.json`), recurrence phrases
  (`recurrence-text.json`) and which stored rules are valid
  (`rrule-validity.json`).

The tests that consume them live beside the packages they check
(`internal/mdwal`, `internal/chrono`, `internal/rrule`, `internal/app`).
