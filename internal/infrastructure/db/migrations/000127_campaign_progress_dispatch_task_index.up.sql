-- A campaign task counts as a send only when it holds a step's reservation
-- (issue #306): the chain's wake-ups complete without sending and must not
-- spend the mailbox's daily budget. The counters look the reservation up by
-- its task, so that lookup needs an index. Built concurrently, on its own,
-- because progress is a live table and a plain CREATE INDEX would block it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_campaign_progress_dispatch_task
    ON campaign_contact_progress (dispatch_task_id)
    WHERE dispatch_task_id IS NOT NULL;
