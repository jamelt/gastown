# Wisp GC containment, recovery, and rollback

Live Gas Town patrols must never run forced age-based wisp garbage collection.
Age is not proof that a record is abandoned: hooked work, active work, blocked
steps, dependency waiters, live parents, recovery records, and pending merge
requests can all remain unchanged longer than an hour.

## Normal patrol cleanup

Witness, Refinery, and Deacon patrols may clean closed wisps only. Each patrol
must first run the closed-only JSON dry run, retain its output as the deletion
digest, review that every candidate is closed, and only then run the matching
closed-only forced cleanup. If the preview fails, is unavailable, or includes a
non-closed record, skip mutation and escalate.

There is deliberately no patrol fallback to age-based cleanup. Stale open-wisp
cleanup remains disabled until Beads offers a primitive that supplies explicit
ownership/scope evidence, protects all live graph states, and binds the mutation
to the reviewed dry-run digest.

## Incident containment

If an unsafe GC command runs:

1. Stop the patrol or wake path that issued it. Do not retry GC or run another
   cleanup in an attempt to correct the first one.
2. Record the actor, working directory/database, command, start and finish time,
   stdout/stderr, and reported row counts. Preserve the terminal transcript.
3. Prevent further mutation of the affected database while recovery evidence is
   gathered. Do not compact or prune Dolt history.
4. Escalate as a data-loss incident and identify affected issue, dependency,
   label, comment, and event tables. Treat mail wisps and pending merge records
   as live until proven otherwise.

## Recovery

Perform recovery in an isolated disposable database, never directly against the
live shared Dolt server.

1. Restore the latest validated pre-incident backup or Dolt revision into the
   isolated database.
2. Compare pre- and post-incident rows by stable ID across wisps/issues,
   dependencies, labels, comments, and events. Produce a reviewable restore
   manifest with counts and IDs.
3. Verify that hooked, in-progress, blocked, dependency-waiting, parent-live,
   recovery, and pending-MR records retain both their status and graph edges.
4. Reconcile legitimate changes made after the restore point. Validate hooks,
   ready/blocked results, pending merge requests, and agent recovery state before
   any production cutover.
5. Have a second operator review the manifest and validation evidence, then use
   the normal Dolt restore procedure for the affected deployment.

## Rollback of this containment

Do not roll back by restoring age-based GC to a live patrol. If closed-only GC
causes trouble, the safe rollback is to disable patrol GC entirely while keeping
the preview and incident evidence. Age-based cleanup may return only after the
Beads primitive described above exists and incident-scale regression coverage
proves zero live deletions.
