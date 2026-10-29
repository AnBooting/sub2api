"""Reject missing, failed or skipped statistics PostgreSQL regressions."""
import json
import sys

required = {
    "TestReview29DashboardPartialHistoryKeepsSafeToday",
    "TestReview29DashboardSnapshotKeepsRangeCheckAndReadTogether",
    "TestReview29GroupWatermarkMustNotCrossNewlyQuarantinedGap",
    "TestReview29GroupSummaryMustRejectQuarantinedTail",
    "TestReview29UpgradeDayMustNotSilentlySkipDashboardAggregation",
    "TestReview29FullMigrationRunner",
    "TestReview29DashboardContinuesAfterUpgradeGap",
    "TestReview29DashboardReadersRejectQuarantine",
    "TestReview29GroupSummaryCompleteness",
    "TestReview29QueueFailPreservesRetentionQuarantine",
    "TestReview29RepairTriggerAndManualDeletion",
    "TestReview29QueueReclaimFencesOldWorker",
    "TestReview29GroupContinuousPrefixAndExplicitReconciliation",
}
passed = set()
rejected = []
with open(sys.argv[1], encoding="utf-8") as stream:
    for line in stream:
        event = json.loads(line)
        test = event.get("Test", "")
        if not test.startswith("TestReview29"):
            continue
        if event.get("Action") in {"fail", "skip"}:
            rejected.append(test)
        if event.get("Action") == "pass" and "/" not in test:
            passed.add(test)
missing = required - passed
if rejected or missing:
    sys.exit(f"PostgreSQL regressions did not execute successfully: skipped/failed={rejected}, missing={sorted(missing)}")
print(f"PostgreSQL regressions: {len(passed)} top-level tests passed, zero skips")
