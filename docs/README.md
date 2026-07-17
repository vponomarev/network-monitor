# Documentation Index

This index separates maintained operational documentation from historical
plans and reports. For status or prioritization decisions, always use
[`STATUS_AND_PLAN.md`](STATUS_AND_PLAN.md).

## Current documentation

### Status and operations

- [`STATUS_AND_PLAN.md`](STATUS_AND_PLAN.md) — supported scope, completed work,
  and ordered backlog.
- [`PRODUCTION_ru.md`](PRODUCTION_ru.md) and
  [`PRODUCTION_en.md`](PRODUCTION_en.md) — production deployment of `netmon`.
- [`CONNTRACK.md`](CONNTRACK.md) — standalone conntrack behavior and metrics.
- [`installation.md`](installation.md), [`configuration.md`](configuration.md),
  and [`api-reference.md`](api-reference.md) — operator references.
- [`RELEASE_PROCESS.md`](RELEASE_PROCESS.md) — Linux `amd64` release and
  qualification procedure.

### Engineering

- [`architecture.md`](architecture.md) and [`ebpf-guide.md`](ebpf-guide.md) —
  architecture and eBPF development.
- [`development.md`](development.md), [`TESTING_GUIDE.md`](TESTING_GUIDE.md), and
  [`CICD_GUIDE.md`](CICD_GUIDE.md) — development, test, and CI workflows.
- [`DOCKER_DEPLOYMENT.md`](DOCKER_DEPLOYMENT.md) — container deployment.
- [`SYSTEMD_DEPLOYMENT.md`](SYSTEMD_DEPLOYMENT.md) — legacy `trace_pipe`
  reference; use the production guides for current eBPF units.

## Historical documentation

The following files describe completed milestones or point-in-time test
results. They are kept for traceability and must not be interpreted as an open
backlog:

- `prod-readiness/` — Phase 1 production-readiness task set;
- `MVP_STATUS.md`, `development-plan.md`, and `SINGLE_BINARY_PLAN.md`;
- `PHASE2_STATUS.md`, `PHASE2B_TRACEROUTE.md`, `PHASE2C_INTEGRATION.md`, and
  `PHASE3_TOPOLOGY.md`;
- `DEPLOYMENT_REPORT.md`, `DEBIAN12_TEST_REPORT.md`,
  `CONNTRACK_OUTGOING_FIX.md`, `REAL_DATA_ANALYSIS.md`, and `TEST_COVERAGE.md`.

[`PR_3_RECONCILIATION.md`](PR_3_RECONCILIATION.md) records how the unmerged
Phase 2 plan from PR #3 maps to the current codebase and roadmap.
