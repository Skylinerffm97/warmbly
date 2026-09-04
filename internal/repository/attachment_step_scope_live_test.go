package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// A campaign attachment carries an optional sequence_id, and the send path now
// honours it: a step sends the campaign-wide files plus its own, and nothing
// scoped to another step. Cover for the scoping query itself, plus the guard
// that refuses a step_id belonging to another campaign (the FK only proves the
// step exists).
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAttachmentStepScope -v
func TestLiveAttachmentStepScope(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewAttachmentRepository(handle)
	ctx := context.Background()

	stepOne, stepTwo, otherCampaignStep := uuid.New(), uuid.New(), uuid.New()
	for _, s := range []struct {
		id       uuid.UUID
		campaign uuid.UUID
		position int
	}{{stepOne, f.campaign, 0}, {stepTwo, f.campaign, 1}, {otherCampaignStep, f.other, 0}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_html, body_plain, position)
			 VALUES ($1, $2, $3, 'Step', 'Subject', '<p>Body</p>', 'Body', $4)`,
			s.id, s.campaign, f.org, s.position); err != nil {
			t.Fatalf("fixture sequence: %v", err)
		}
	}

	insert := func(name string, step *uuid.UUID) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`INSERT INTO campaign_attachments (campaign_id, sequence_id, user_id, filename, size, mime_type, s3_key)
			 VALUES ($1, $2, $3, $4, 10, 'application/pdf', $5)`,
			f.campaign, step, f.owner, name, "live/"+name); err != nil {
			t.Fatalf("fixture attachment %s: %v", name, err)
		}
	}
	insert("everystep.pdf", nil)
	insert("stepone.pdf", &stepOne)
	insert("steptwo.pdf", &stepTwo)

	names := func(step uuid.UUID) []string {
		t.Helper()
		atts, err := repo.ListForStep(ctx, f.campaign, step)
		if err != nil {
			t.Fatalf("ListForStep: %v", err)
		}
		out := make([]string, 0, len(atts))
		for _, a := range atts {
			out = append(out, a.Filename)
		}
		return out
	}

	same := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	if got := names(stepOne); !same(got, []string{"everystep.pdf", "stepone.pdf"}) {
		t.Errorf("step one sends %v, want the campaign-wide file and its own", got)
	}
	if got := names(stepTwo); !same(got, []string{"everystep.pdf", "steptwo.pdf"}) {
		t.Errorf("step two sends %v, want the campaign-wide file and its own", got)
	}
	// A step of the campaign that owns no files still carries the campaign-wide
	// ones, and uuid.Nil (no step in context) carries only those.
	if got := names(uuid.Nil); !same(got, []string{"everystep.pdf"}) {
		t.Errorf("unscoped list is %v, want the campaign-wide file alone", got)
	}

	// Every file of the campaign is still listed for the dashboard.
	all, err := repo.ListByCampaign(ctx, f.campaign)
	if err != nil {
		t.Fatalf("ListByCampaign: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListByCampaign returned %d files, want all 3", len(all))
	}

	for _, tc := range []struct {
		name string
		step uuid.UUID
		want bool
	}{
		{"own step", stepOne, true},
		{"another campaign's step", otherCampaignStep, false},
		{"unknown step", uuid.New(), false},
	} {
		got, err := repo.StepBelongsToCampaign(ctx, f.campaign, tc.step)
		if err != nil {
			t.Fatalf("StepBelongsToCampaign(%s): %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("StepBelongsToCampaign(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
