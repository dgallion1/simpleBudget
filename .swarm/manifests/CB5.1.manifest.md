# CB5.1 manifest (worker=lead, lean exception 2026-08-31)

- internal/handlers/dashboard/handlers.go — buildMajorExpenseChartData:
  Unmatched with transactions and total <= 0 joins the credits list,
  appended after the matched credit buckets (contract in
  .swarm/CB5-RUN-SPEC.md, extending CB4-2026-09-02a). The positive-wedge
  guard is unchanged; doc comment block updated inline at the credits
  construction.
- internal/handlers/dashboard/handlers_http_test.go — three new tests:
  TestBuildMajorExpenseChartData_UnmatchedNetRefundGoesToCredits,
  _UnmatchedZeroWithTxnsGoesToCredits,
  _UnmatchedCreditOrderedAfterGroupCredits.

Lead verification before checker dispatch: go build ./... clean,
go vet ./... clean, full go test -count=1 ./... zero non-ok packages.
