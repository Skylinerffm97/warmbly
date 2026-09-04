package tasks

import (
	"context"
	"testing"
)

// End-to-end checks for issue #306 over the real campaign tick: the chain's
// own wake-ups (a deferral, a pause) must not spend the mailbox's daily
// budget, and a campaign whose mailboxes are all at their cap waits for
// tomorrow instead of pausing. Same harness and env var as
// campaign_send_live_test.go.

// setCaps sets the per-mailbox cap on both the mailbox and the campaign, so
// the effective cap is exactly n.
func (f *sendFixture) setCaps(t *testing.T, n int) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET campaign_limit = $2 WHERE id = $1`, f.mailbox, n); err != nil {
		t.Fatalf("set mailbox cap: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE campaigns SET daily_limit = $2 WHERE id = $1`, f.campaign, n); err != nil {
		t.Fatalf("set campaign cap: %v", err)
	}
}

func (f *sendFixture) setMinGap(t *testing.T, seconds int) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE email_accounts SET min_wait_time = $2 WHERE id = $1`, f.mailbox, seconds); err != nil {
		t.Fatalf("set min gap: %v", err)
	}
}

func (f *sendFixture) campaignStatus(t *testing.T) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&status); err != nil {
		t.Fatalf("campaign status: %v", err)
	}
	return status
}

func (f *sendFixture) countLogs(t *testing.T, eventType string) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = $2`,
		f.campaign, eventType).Scan(&n); err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return n
}

// TestLiveDeferredTicksDoNotSpendTheDailyBudget: one send, then three ticks
// that defer on the mailbox's min-gap, then the second lead must still go out.
// Before the fix the three deferrals were three sends against a cap of two,
// and the fourth tick paused the campaign with a lead never emailed.
func TestLiveDeferredTicksDoNotSpendTheDailyBudget(t *testing.T) {
	f := newSendFixture(t)
	f.setCaps(t, 2)

	f.tick(t)
	if f.sender.count() != 1 {
		t.Fatalf("first tick dispatched %d sends, want 1", f.sender.count())
	}

	// A 600s gap after that send: every tick inside it defers without sending.
	f.setMinGap(t, 600)
	for i := 0; i < 3; i++ {
		f.tick(t)
	}
	if f.sender.count() != 1 {
		t.Fatalf("deferred ticks dispatched sends: total %d, want 1", f.sender.count())
	}
	if status := f.campaignStatus(t); status != "active" {
		t.Fatalf("campaign is %q after three deferrals, want active", status)
	}
	sent, err := f.svc.taskRepo.CountCampaignEmailsSentToday(context.Background(), f.mailbox)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("the mailbox is charged %d sends today, want 1 (the deferrals were counted, issue #306)", sent)
	}

	// Gap lifted: the second lead is still within budget and goes out.
	f.setMinGap(t, 0)
	f.tick(t)
	if f.sender.count() != 2 {
		t.Fatalf("the second lead was not sent after the deferrals (total %d); campaign is %q", f.sender.count(), f.campaignStatus(t))
	}
	if row := f.progressFor(t, f.leadB); row == nil || row.sentAt == nil {
		t.Fatalf("lead B was not served: %+v", row)
	}
}

// TestLiveCampaignAtDailyCapWaitsForTomorrow: with the cap spent the campaign
// stays active with a parked wake-up, and says why once.
func TestLiveCampaignAtDailyCapWaitsForTomorrow(t *testing.T) {
	f := newSendFixture(t)
	f.setCaps(t, 1)
	ctx := context.Background()

	f.tick(t)
	if f.sender.count() != 1 {
		t.Fatalf("first tick dispatched %d sends, want 1", f.sender.count())
	}

	for i := 0; i < 2; i++ {
		f.tick(t)
		if status := f.campaignStatus(t); status != "active" {
			t.Fatalf("tick %d at the daily cap left the campaign %q, want active (it used to be paused_no_accounts)", i+2, status)
		}
	}
	if f.sender.count() != 1 {
		t.Fatalf("ticks at the cap dispatched sends: total %d, want 1", f.sender.count())
	}

	// The chain is parked, not dropped.
	var pending int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks t JOIN campaign_tasks ct ON ct.task_id = t.id
		WHERE ct.campaign_id = $1 AND t.status = 'pending'`, f.campaign).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("%d pending wake-ups after the cap was reached, want 1", pending)
	}
	if n := f.countLogs(t, "daily_cap_reached"); n != 1 {
		t.Fatalf("daily_cap_reached logged %d times over two ticks, want once per day", n)
	}
	if n := f.countLogs(t, "auto_paused"); n != 0 {
		t.Fatalf("the campaign was auto-paused %d time(s) at its daily cap", n)
	}
}
