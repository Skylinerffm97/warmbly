package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks for issue #306: the per-mailbox daily budget counted every
// completed campaign task as a sent email, so the chain's own wake-ups (a
// deferral, a pause) spent the budget and reset the min-gap clock, and a pool
// with every mailbox at its cap paused the campaign instead of waiting for
// tomorrow. Same harness and env var as live_integration_test.go.

// completeCampaignTask writes one campaign task the mailbox completed today.
// With a step it is a send: the step's reservation points at the task, the way
// ReserveSend leaves it. Without one it is a wake-up that sent nothing.
func (f *liveFixture) completeCampaignTask(t *testing.T, contact, step *uuid.UUID, completedAt time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	taskID := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, completed_at, created_at, updated_at)
		VALUES ($1, 'campaign', $2, 'completed', '', $3, $3, $3, $3)`, taskID, f.mailbox, completedAt); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id)
		VALUES ($1, $2, $3, $4)`, taskID, f.campaign, contact, step); err != nil {
		t.Fatalf("link task: %v", err)
	}
	if contact != nil && step != nil {
		if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_contact_progress
			(campaign_id, contact_id, sequence_id, sent_at, dispatched_at, dispatch_task_id)
			VALUES ($1, $2, $3, $4, $4, $5)`, f.campaign, *contact, *step, completedAt, taskID); err != nil {
			t.Fatalf("reserve step: %v", err)
		}
	}
	return taskID
}

// addSentLead attaches a second lead whose first step this mailbox already
// sent today, so the pool has one real send on the books while the fixture's
// own lead is still waiting to go.
func (f *liveFixture) addSentLead(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var step uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT id FROM sequences WHERE campaign_id = $1 ORDER BY position LIMIT 1`, f.campaign).Scan(&step); err != nil {
		t.Fatalf("find step: %v", err)
	}
	contact := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
		VALUES ($1, $2, $3, $4, 'Sent', 'Lead', '', '', '{}')`,
		contact, f.user, f.org, "sent-"+contact.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("add contact: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 1)`,
		f.campaign, contact); err != nil {
		t.Fatalf("link lead: %v", err)
	}
	f.completeCampaignTask(t, &contact, &step, time.Now().Add(-2*time.Hour))
}

// loggedScheduler is liveScheduler with the activity log wired, for the
// checks on what a deferral records.
func loggedScheduler(t *testing.T, f *liveFixture) SchedulerService {
	t.Helper()
	handle, pool := liveDB(t)
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM campaign_logs WHERE campaign_id = $1`, f.campaign)
	})
	return NewSchedulerService(
		repository.NewTaskRepository(pool),
		repository.NewWarmupRepository(pool),
		repository.NewCampaignProgressRepository(pool),
		repository.NewEmailRepostory(handle, enc),
		repository.NewCampaignRepostory(handle),
		repository.NewContactRepostory(handle),
		repository.NewCampaignLogRepository(handle),
	)
}

// TestLiveWakeupsDoNotSpendTheDailyBudget: a mailbox whose chain woke up fifty
// times today without sending has its whole budget left, and no min-gap to
// wait out.
func TestLiveWakeupsDoNotSpendTheDailyBudget(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	// Fifty completed wake-ups, the last one seconds ago: exactly the cap, and
	// inside the 600s min-gap.
	for i := 0; i < 50; i++ {
		f.completeCampaignTask(t, nil, nil, time.Now().Add(-time.Duration(i)*time.Second))
	}

	taskRepo := repository.NewTaskRepository(pool)
	sent, err := taskRepo.CountCampaignEmailsSentToday(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if sent != 0 {
		t.Fatalf("CountCampaignEmailsSentToday = %d for a mailbox that sent nothing (issue #306)", sent)
	}
	last, err := taskRepo.GetLastEmailTime(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("last email time: %v", err)
	}
	if last != nil {
		t.Fatalf("GetLastEmailTime = %s for a mailbox that sent nothing; a wake-up reset the min-gap clock", last)
	}

	at, pair, accountID, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(ctx, f.campaign)
	if err != nil {
		t.Fatalf("the campaign should be sendable, got %v", err)
	}
	if pair == nil || accountID != f.mailbox {
		t.Fatalf("no sendable pair from the fixture mailbox: pair=%v account=%s", pair, accountID)
	}
	assertFuture(t, at)
}

// TestLiveRealSendsStillSpendTheDailyBudget is the other half: the counter has
// to keep seeing the sends it exists for, both the reserved one and the one
// only the worker's Message-ID vouches for.
func TestLiveRealSendsStillSpendTheDailyBudget(t *testing.T) {
	_, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	f.addSentLead(t)
	// A send whose reservation is gone but whose task carries the worker's
	// Message-ID (a step walked back and re-sent, or one that predates the
	// reservation).
	confirmed := f.completeCampaignTask(t, nil, nil, time.Now().Add(-time.Hour))
	if _, err := pool.Exec(ctx, `UPDATE tasks SET message_id = '<confirmed@test.local>' WHERE id = $1`, confirmed); err != nil {
		t.Fatal(err)
	}
	// And one wake-up, which must not count.
	f.completeCampaignTask(t, nil, nil, time.Now().Add(-time.Minute))

	taskRepo := repository.NewTaskRepository(pool)
	sent, err := taskRepo.CountCampaignEmailsSentToday(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if sent != 2 {
		t.Fatalf("CountCampaignEmailsSentToday = %d, want 2 (the reserved send and the confirmed one)", sent)
	}
	last, err := taskRepo.GetLastEmailTime(ctx, f.mailbox)
	if err != nil {
		t.Fatalf("last email time: %v", err)
	}
	if last == nil || time.Since(*last) < 50*time.Minute || time.Since(*last) > 70*time.Minute {
		t.Fatalf("GetLastEmailTime = %v, want the confirmed send an hour ago, not the wake-up a minute ago", last)
	}
}

// TestLiveDailyCapDefersInsteadOfPausing: with every mailbox at its cap the
// campaign waits for tomorrow, says so once, and is never paused.
func TestLiveDailyCapDefersInsteadOfPausing(t *testing.T) {
	_, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `UPDATE campaigns SET daily_limit = 1 WHERE id = $1`, f.campaign); err != nil {
		t.Fatal(err)
	}
	f.addSentLead(t)

	s := loggedScheduler(t, f)
	at, pair, _, err := s.CalculateNextCampaignTime(ctx, f.campaign)
	if errors.Is(err, ErrNoEmailAccounts) {
		t.Fatalf("a pool at its daily cap paused the campaign: %v", err)
	}
	if !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("want ErrCampaignDeferred, got err=%v pair=%v", err, pair)
	}
	if pair != nil {
		t.Fatal("a deferral must never hand back a sendable pair")
	}
	if !at.After(time.Now().Add(23 * time.Hour)) {
		t.Fatalf("deferred to %s, want tomorrow", at)
	}

	// A second pass on the same day logs nothing new.
	if _, _, _, err := s.CalculateNextCampaignTime(ctx, f.campaign); !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("second pass: %v", err)
	}
	var logged int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = 'daily_cap_reached'`,
		f.campaign).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Fatalf("daily_cap_reached logged %d times over two passes, want once per day", logged)
	}
}
