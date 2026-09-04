package scheduler

import (
	"errors"
	"fmt"
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

var (
	// ErrWarmupNotEnabled is returned when warmup is not enabled for an account
	ErrWarmupNotEnabled = errors.New("warmup not enabled for this account")

	// ErrCampaignNotActive is returned when a campaign is not active
	ErrCampaignNotActive = errors.New("campaign is not active")

	// ErrCampaignCompleted is returned when all emails in a campaign have been sent
	ErrCampaignCompleted = errors.New("campaign completed - no more emails to send")

	// ErrCampaignEnded is returned when a campaign has passed its end date
	ErrCampaignEnded = errors.New("campaign ended - past end date")

	// ErrNoEmailAccounts is returned when no email accounts are available for sending
	ErrNoEmailAccounts = errors.New("no email accounts available for this campaign")

	// ErrNoEligibleMailbox is the narrower case: the campaign HAS mailboxes,
	// but none can send under its current settings, and waiting will not
	// change that (a sending-behaviour profile with no working days). A gate
	// that lifts on its own, such as a spent daily budget, a closed sending
	// window or a warmup health hold, is a deferral instead, never this.
	// Reporting this as ErrNoEmailAccounts sent people looking at their tag
	// configuration for a problem that was never there.
	//
	// It wraps ErrNoEmailAccounts so existing callers that pause the campaign
	// on errors.Is(err, ErrNoEmailAccounts) keep behaving exactly as before.
	ErrNoEligibleMailbox = fmt.Errorf(
		"%w: no mailbox can send under its current sending settings", ErrNoEmailAccounts)

	// ErrDomainAuthFailing is the narrower case again: every mailbox in the
	// campaign's pool was gated by the sending-domain authentication check.
	// Reporting that as ErrNoEligibleMailbox would send people to check
	// timezones and daily caps for a DNS problem, which is exactly the class of
	// mislabelling ErrNoEligibleMailbox was introduced to fix.
	//
	// It wraps ErrNoEmailAccounts so existing callers that pause the campaign
	// keep behaving as before; callers that want the specific reason must test
	// for it BEFORE ErrNoEligibleMailbox and ErrNoEmailAccounts.
	ErrDomainAuthFailing = fmt.Errorf(
		"%w: every mailbox is sending from a domain that fails SPF/DMARC authentication", ErrNoEmailAccounts)

	// ErrDailyLimitReached is returned when the daily limit has been reached
	ErrDailyLimitReached = errors.New("daily email limit reached")

	// ErrCampaignDeferred is returned when there IS a valid contact to send but
	// no eligible mailbox right now — ESP-strict has no same-provider mailbox
	// under budget, or the daily new-lead cap is reached. The caller must
	// reschedule at the returned (defer) time WITHOUT sending. The returned pair
	// is always nil on this path so it can never be mistaken for a sendable
	// contact; the returned accountID is a nominal pool mailbox for the wakeup
	// task only (the next invocation re-evaluates selection from scratch).
	ErrCampaignDeferred = errors.New("campaign send deferred - no eligible mailbox for this contact right now")
)

// DeferSlot is the wakeup time a caller must use after CalculateNextCampaignTime
// returns ErrCampaignDeferred. The returned instant is the campaign's real
// next-due moment, which is the honest answer to "when could this send" but the
// wrong answer to "when should this chain look again": a campaign is one
// self-perpetuating task, so parking it at a next-due three days out also means
// nothing re-reads the campaign for three days. Leads imported in the meantime
// sit at "Queued / Not started" until then.
//
// So a deferral is capped at config.CampaignMaxDeferMinutes. Anything sooner is
// kept as-is, because a near-term defer is already a precise wakeup. Sends are
// unaffected: a tick that fires early and still has nothing due simply defers
// again, and a tick that DID send parks its successor at the paced interval,
// which never goes through here.
func DeferSlot(at time.Time) time.Time {
	horizon := time.Now().Add(config.CampaignMaxDeferMinutes * time.Minute)
	if at.IsZero() || at.After(horizon) {
		return horizon
	}
	return at
}
