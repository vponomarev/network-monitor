# PR #3 Reconciliation

PR #3 (`phase-2-conntrack`) was opened before the conntrack production work and
contains a detailed Phase 2 plan. It was reviewed against `main` after the
v2.2.0 release. Direct merging is unsafe because its base, status statements,
kernel assumptions, CI comments, and test comments predate the implemented
solution.

## Incorporated outcome

| Original area | Current outcome |
|---|---|
| Phase 1 archive | Classified as historical in `docs/README.md` |
| CO-RE/layout fixes | Completed and qualified on kernels 5.15–6.12 |
| Remove `bpf_printk` and ring-buffer busy-loop | Completed |
| PID/comm hot-path optimization | Completed |
| Self-metrics, health, readiness, no simulation | Completed |
| Standalone configuration and production packaging | Completed |
| Conntrack E2E restoration | Replaced by repeatable host/matrix E2E scripts |
| IPv6 | Still planned; outside current P0 scope |
| Kernel 6.17 qualification | Not part of the available supported matrix |

The current backlog is defined only in `STATUS_AND_PLAN.md`. Retention, bounded
state, automated soak qualification, and safe upgrade/rollback are implemented
in the current work; IPv6 remains deferred. The non-documentation changes in
PR #3 only update links to a proposed directory move and are therefore not
required on `main`.
