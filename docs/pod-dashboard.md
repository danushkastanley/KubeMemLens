# Pod dashboard

From the existing Pods dashboard, select a Pod and press **Enter** (or `e`).
The dedicated Pod dashboard uses the same terminal theme and leaves the parent
view intact. **Esc**, `h` or Backspace returns to the previous view with its
filter, sort, selection and scroll position retained. If a refresh reorders the
rows, the selected Pod is restored by namespace/name. If it has disappeared,
the nearest remaining row is selected.

At 150 columns × 40 rows and above, the dashboard shows:

- Pod charge, configured limit, headroom, current signals and finding;
- Pod context and evidence freshness;
- non-overlapping anonymous/file-cache/shmem/residual composition;
- bounded history for the current Pod instance;
- explanation, read-only recommendations, comparison and raw evidence actions;
- container contributions and recent event/pressure evidence.

Smaller terminals show one panel at a time. Use **Tab / Shift+Tab** to change
panels; `j/k` or arrows to scroll or select; PgUp/PgDown to page; and `g/G` to
reach the beginning/end. Panel titles and focus borders identify the active
panel without relying on colour. **Enter** in the checks panel opens its selected
action; Enter in the containers panel opens the selected container's detail.
Esc closes a nested preview/detail before returning to the main dashboard.

Press `e` inside the Pod dashboard for the complete line-oriented evidence,
including secondary kernel, volume and MemoryQoS details. `R` opens read-only
recommendations, `x` starts the existing two-Pod comparison, Space pauses
refresh, `r` refreshes once, `?` explains controls, and `q` quits. Filtering and
sorting remain in the parent table.

The history plot uses a zero baseline and observed samples positioned by source
time. It does not interpolate across missing samples or combine different Pod
instances. The current configured limit is a scale reference, not historical
limit evidence. A failed history refresh retains explicitly labelled last-good
history; no history is drawn when the matching Pod instance has no points.

Connection loss, paused refresh, stale snapshots and incomplete evidence remain
visible. Unknown/unlimited limits are not shown as zero; event counts require a
known, complete delta window. The UI does not run suggested shell commands or
mutate Kubernetes workloads. `NO_COLOR` remains supported. This change does not
alter the main dashboard layout, collector/API contracts or release version.
