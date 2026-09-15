# Grafana Dashboard Design Document: Node Readiness Controller

### LFX Project: Granular Metrics and Improving SLIs from NRC — Dashboard Companion

This document is the dashboard-side counterpart to the observability design doc (docs/observability-design.md, PR #344). That doc decides what gets emitted; this doc decides what gets shown, to whom, and why. It follows the same three personas and the same SLI/SLO table, and uses only the new/reshaped metric surface from docs/observability-design.md §3 — no metric slated for rename, reshape, or deletion in §5 appears in a query here.

## Scope

- One Grafana dashboard, provisioned as JSON in-repo (`config/grafana/dashboards/`), covering **controller metrics only**.
- Built section-by-section, one PR per section, same workflow as the existing Section 1/2/3 work.
- **Section 1 is reserved: PR #325's panel set is locked in as-is.** That PR is on hold only because it queries the old metric names — the panel set itself (Total Managed Nodes, Blocked Nodes, Fleet Availability %, Rules Without Nodes) is not being revisited. When the underlying metrics land, #325 gets its queries swapped and merges; nothing new gets added to that section.

## Non-goals

- **Reporter (readiness-condition-reporter) metrics are not on this dashboard.** Confirmed with Ajay: node-side churn makes centralized dashboarding impractical for the reporter; it stays out entirely.
- No Headlamp plugin content — that's a separate deliverable, out of LFX scope.
- No per-node panels or labels. Consistent with docs/observability-design.md's cardinality rule, nothing here breaks out by node name.

## Design principles

1. **One question per panel, one primary question per section.** A panel that doesn't answer a question a real persona asks doesn't go on the dashboard, no matter how easy it is to build from an existing metric.
2. **Golden-signal row on top, breakdown below, drill-down at the bottom.** Every section opens with the panels an operator needs at a glance; deeper detail lives in a later section rather than getting crammed into the summary tier.
3. **No hardcoded rule names anywhere.** Every `$rule` variable uses `label_values()` scoped to the dashboard's time range, exactly like the hardened pattern from Section 2/3.
4. **Sparse-data guarding is mandatory**, not optional, on every `histogram_quantile` and every ratio — the established `and on(rule) (sum by (rule) (rate(..._count[window])) > 0)` pattern, or the "no data" sentinel/value-mapping pattern from Section 3, whichever fits the panel.
5. **Old metric names never appear.** `node_readiness_rules_total`, `nodes_by_state`, `bootstrap_duration_seconds`, `reconciliation_latency_seconds`, `condition_failures_total`, `rule_last_reconciliation_timestamp_seconds` are all superseded per docs/observability-design.md §5 and are excluded from every query on this dashboard, even where dual-publishing keeps them alive during alpha.
6. **`node_readiness_api_conflicts_total` is excluded for now**, consistent with the earlier Wave 2 decision to drop it — it reappeared in docs/observability-design.md but hasn't been re-confirmed with Ajay, so no panel depends on it. Revisit after that discussion.

## Personas (from docs/observability-design.md §1 — unchanged, restated for reference)

| Persona                                   | Core question                                                    |
| ----------------------------------------- | ---------------------------------------------------------------- |
| Infrastructure Owners (Cluster Operators) | Is the fleet healthy? Is the controller working?                 |
| Component / Rule Owners                   | Is my component blocking node readiness? Which check is failing? |
| Workload Owners (Application developers)  | How long does readiness gating delay my pods?                    |

---

## Dashboard structure

Five sections. Section 1 is fixed by PR #325. Sections 2–5 are organized so every panel that PR #325 deliberately _doesn't_ carry — per-rule breakdown, rule inventory, taint churn, build info — has an explicit home, instead of being squeezed back into the fixed 4-panel summary tier.

### Section 1 — Fleet Overview (Infrastructure Owners, headline tier) — locked to PR #325

**Answers:** is the fleet healthy right now, at a glance. Four panels, no more, no less — reserved from #325.

| Panel                                     | Viz  | New metric                                                  | Old metric it replaces                                              |
| ----------------------------------------- | ---- | ----------------------------------------------------------- | ------------------------------------------------------------------- |
| Total Managed Nodes                       | Stat | `sum(node_readiness_rule_nodes)` (held+released)            | `node_readiness_nodes_by_state`                                     |
| Blocked Nodes                             | Stat | `sum(node_readiness_rule_nodes{state="held"})`              | `node_readiness_nodes_by_state{state="held"}`                       |
| Fleet Availability %                      | Stat | released ÷ (held+released) from `node_readiness_rule_nodes` | same, old metric                                                    |
| Rules Without Nodes (Misconfigured Rules) | Stat | count of `node_readiness_rule_matched_nodes == 0`           | `node_readiness_selector_matched_nodes_total` (PR #286, pre-rename) |

Only the four queries above change when #431/#286-successor land. Panel titles, layout, and count stay exactly as shipped in #325's screenshot.

### Section 2 — Fleet Detail (Infrastructure Owners, drill-down tier)

**Answers:** the same "is the fleet healthy" question as Section 1, one level deeper — which rule is holding nodes, is anything flapping, what's actually configured. This section exists specifically to hold what Section 1 deliberately excludes.

| Panel                        | Viz         | Query / metric                                                        | Notes                                                                                                     |
| ---------------------------- | ----------- | --------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Held/Released by Rule        | Table       | `node_readiness_rule_nodes` by rule, state                            | Sortable, worst-held-first                                                                                |
| Rule Inventory               | Table       | `node_readiness_rules{enforcement_mode,dry_run}`                      | Dry-run vs. enforcing rule counts — new, wasn't possible before this metric existed                       |
| Taint Churn (flapping check) | Time series | `rate(node_readiness_taint_operations_total[30m])` by rule, operation | Continuous add/remove cycling shows as a sawtooth. Reused (not re-defined) by Section 4's flapping check. |

### Section 3 — Rule Health & Blocking (Component / Rule Owners)

**Answers:** which component is blocking readiness, and which rule is the worst offender.

| Panel                   | Viz                       | Query / metric                                                                                                                   | Notes                                                                                                                                                                                                        |
| ----------------------- | ------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Blocking Conditions     | Table                     | `max by (rule, condition) (node_readiness_blocked_nodes)`, sorted desc                                                           | The flagship new panel — nothing like it exists on any prior dashboard iteration. Most direct answer to "is my component blocking readiness."                                                                |
| Rule Bottleneck Ranking | Table                     | rule, p99 reaction time (`node_readiness_enforcement_latency_seconds`), failure % (`node_readiness_failures_total`), taint churn | Carries forward the validated Section 3 (old numbering) table design — same columns, same sparse-data guard, re-pointed at `enforcement_latency_seconds` instead of the old `reconciliation_latency_seconds` |
| Selector Match          | Table                     | `node_readiness_rule_matched_nodes` by rule                                                                                      | Same metric as Section 1's misconfiguration stat, here `$rule`-filterable for drill-down                                                                                                                     |
| Evaluation Duration     | Time series (p50/p95/p99) | `node_readiness_evaluation_duration_seconds`                                                                                     | Per-rule, `$rule`-filterable                                                                                                                                                                                 |
| Failure Breakdown       | Table or stacked bar      | `node_readiness_failures_total` by rule, reason                                                                                  | Uses docs/observability-design.md's fixed reason vocabulary — one row/segment per reason string                                                                                                              |

### Section 4 — Workload Impact (Workload Owners)

**Answers:** how much is readiness gating actually delaying my pods.

| Panel                                 | Viz                           | Query / metric                                                                                                      | Notes                                                                                                                                                                                                                      |
| ------------------------------------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Bootstrap Hold Duration (p50/p95/p99) | Time series                   | `histogram_quantile(..., node_readiness_bootstrap_hold_duration_seconds)`, split by `taint_origin`                  | PR #294's metric folds into this histogram as `taint_origin="controller"` alongside docs/observability-design.md's `"adopted"` values — one panel covers controller-caused vs. adopted-at-boot hold time on the same chart |
| Bootstrap Hold Distribution           | Heatmap                       | `node_readiness_bootstrap_hold_duration_seconds_bucket`                                                             | Distribution view alongside the percentile lines above — percentiles alone can hide bimodal behavior a heatmap surfaces immediately                                                                                        |
| SLO Compliance                        | Table (sortable, worst-first) | % of nodes under target threshold, by rule                                                                          | Worst-first table, not bar gauges — validated not to scale past a few dozen rules                                                                                                                                          |
| Release Latency                       | Stat                          | `node_readiness_enforcement_latency_seconds{operation="remove"}` p99 vs. docs/observability-design.md's <60s target | Direct SLO-vs-actual comparison                                                                                                                                                                                            |
| Flapping Check                        | Time series                   | same query as Section 2's taint churn, reused not rebuilt                                                           |                                                                                                                                                                                                                            |

### Section 5 — Controller Internals (shared / debugging)

**Answers:** is the controller process itself keeping up, independent of any single rule.

| Panel                 | Viz         | Query / metric                              | Notes                                                     |
| --------------------- | ----------- | ------------------------------------------- | --------------------------------------------------------- |
| Controller Up         | Stat        | `up{job="nrr-metrics-service"}`             | From controller-runtime, not NRC's own registry           |
| Workqueue Depth       | Time series | `workqueue_depth`                           | docs/observability-design.md's own stated alert: `> 1000` |
| Workqueue Latency     | Time series | `workqueue_queue_duration_seconds`          | Saturation signal                                         |
| Reconcile Errors      | Time series | `controller_runtime_reconcile_errors_total` |                                                           |
| Bootstrap Completions | Stat        | `node_readiness_bootstrap_completed_total`  | Small context panel, not a headline number                |
| Build Info            | Stat/table  | `node_readiness_build_info`                 | Version-skew tracking across replicas                     |

This section sources from `controller-runtime`/`kube-state-metrics`, not the `node_readiness_` namespace, except for the two NRC context panels — per docs/observability-design.md's "Other Documented Metrics (not owned by NRC)" list, worth calling out in the PR description so reviewers don't ask why most of this section isn't an NRC metric.

---

## Layout conventions

- Each section is its own dashboard **row**, collapsible, so an operator can collapse everything except the persona/tier they care about.
- Section 1's four panels are never collapsed by default — they're the landing view.
- `$rule` variable: multi-select, `label_values()`, refresh "On Time Range Change," no default selection, no hardcoded allowlist — same hardened pattern as the original Section 2/3 work, applied dashboard-wide instead of rebuilt per section.
- Default time range: 15 minutes (carried over from the earlier Section 2 fix, to avoid stale demo/diagnostic series lingering in view).

## Metric → panel traceability

| Metric                                                               | Section(s)                                  | Blocked on                                   |
| -------------------------------------------------------------------- | ------------------------------------------- | -------------------------------------------- |
| `node_readiness_build_info`                                          | 5                                           | merged (#406)                                |
| `node_readiness_rules`                                               | 2                                           | #448 open                                    |
| `node_readiness_rule_matched_nodes`                                  | 1, 3                                        | merged (#398, scrape-time collector)         |
| `node_readiness_rule_nodes`                                          | 1, 2                                        | merged (#398)                                |
| `node_readiness_blocked_nodes`                                       | 3                                           | #431 open                                    |
| `node_readiness_taint_operations_total`                              | 2, 4                                        | merged                                       |
| `node_readiness_failures_total`                                      | 2 (Section 1 no longer uses it directly), 3 | #320 open (reason reshape)                   |
| `node_readiness_evaluation_duration_seconds`                         | 3                                           | #447 open (help-string reshape)              |
| `node_readiness_enforcement_latency_seconds`                         | 3, 4                                        | #450 open                                    |
| `node_readiness_bootstrap_hold_duration_seconds`                     | 4                                           | #294 open (incl. `taint_origin` unification) |
| `node_readiness_bootstrap_completed_total`                           | 5                                           | merged                                       |
| `up`, `workqueue_depth`, `controller_runtime_reconcile_errors_total` | 5                                           | not NRC's — already available                |

Section 1 (PR #325) needs only `rule_nodes` and `rule_matched_nodes` — both already merged via #398. That means once #431/#286-successor formalize `rule_matched_nodes` under its final shape, #325 is close to unblocked even before the rest of the dashboard is.

Nothing in this dashboard is blocked on `node_readiness_api_conflicts_total` or any reporter metric.

## Excluded / deferred

- `node_readiness_api_conflicts_total` — pending re-discussion with Ajay (reappeared in docs/observability-design.md after the earlier Wave 2 drop decision).
- Reporter metrics — explicitly out of scope per Ajay's Aug 17 call.
- Headlamp plugin visualizations — separate deliverable, deferred out of LFX scope.

## Open questions

1. SLO Compliance and Release Latency thresholds in Section 4 are still placeholders (matching the earlier unresolved threshold question from the original Section 2/3 work) — need real target numbers, not guesses, before this ships.
2. Section 5's non-NRC panels (`up`, `workqueue_depth`, reconcile errors) — resolved: verified live against the running cluster, all three (`up`, `workqueue_depth`, `controller_runtime_reconcile_errors_total`) are scraped and queryable via `job="nrr-metrics-service"` in the existing Prometheus stack.
