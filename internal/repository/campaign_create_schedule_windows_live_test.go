package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// Regression cover for issue #307 item 5: POST /campaigns accepted
// schedule_windows but Create did not persist the authoritative schedule.
//
// Run against the dev stack:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCreateCampaignPersistsScheduleWindows -v
func TestLiveCreateCampaignPersistsScheduleWindows(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewCampaignRepostory(handle)
	ctx := context.Background()

	t.Run("persists supplied windows", func(t *testing.T) {
		want := models.ScheduleWindows{
			1: []models.TimeInterval{{Start: 540, End: 1020}},
			5: []models.TimeInterval{{Start: 540, End: 840}},
		}
		campaign, xerr := repo.Create(ctx, f.owner.String(), &f.org, &models.CreateCampaign{
			Name:            "Issue 307 schedule windows",
			ScheduleWindows: &want,
		})
		if xerr != nil {
			t.Fatalf("Create: %v", xerr)
		}
		if !reflect.DeepEqual(campaign.ScheduleWindows, want) {
			t.Fatalf("Create schedule_windows = %#v, want %#v", campaign.ScheduleWindows, want)
		}

		var notNull bool
		var stored models.ScheduleWindows
		if err := pool.QueryRow(ctx,
			`SELECT schedule_windows IS NOT NULL, schedule_windows FROM campaigns WHERE id = $1`,
			campaign.ID).Scan(&notNull, &stored); err != nil {
			t.Fatalf("select schedule_windows: %v", err)
		}
		if !notNull {
			t.Fatal("database schedule_windows is NULL, want supplied windows")
		}
		if !reflect.DeepEqual(stored, want) {
			t.Fatalf("database schedule_windows = %#v, want %#v", stored, want)
		}

		got, err := repo.Get(ctx, f.org.String(), campaign.ID.String())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !reflect.DeepEqual(got.ScheduleWindows, want) {
			t.Fatalf("Get schedule_windows = %#v, want %#v", got.ScheduleWindows, want)
		}
	})

	t.Run("keeps legacy schedule null when omitted", func(t *testing.T) {
		campaign, xerr := repo.Create(ctx, f.owner.String(), &f.org, &models.CreateCampaign{
			Name: "Issue 307 legacy schedule",
		})
		if xerr != nil {
			t.Fatalf("Create: %v", xerr)
		}
		if !campaign.ScheduleWindows.IsEmpty() {
			t.Fatalf("Create schedule_windows = %#v, want empty", campaign.ScheduleWindows)
		}

		var isNull bool
		if err := pool.QueryRow(ctx,
			`SELECT schedule_windows IS NULL FROM campaigns WHERE id = $1`,
			campaign.ID).Scan(&isNull); err != nil {
			t.Fatalf("select schedule_windows null state: %v", err)
		}
		if !isNull {
			t.Fatal("database schedule_windows is not NULL, want legacy NULL")
		}
	})

	t.Run("rejects invalid windows without inserting", func(t *testing.T) {
		const name = "Issue 307 invalid schedule"
		bad := models.ScheduleWindows{
			1: []models.TimeInterval{{Start: 600, End: 540}},
		}
		campaign, xerr := repo.Create(ctx, f.owner.String(), &f.org, &models.CreateCampaign{
			Name:            name,
			ScheduleWindows: &bad,
		})
		if xerr == nil {
			t.Fatalf("Create = %#v, nil error; want validation error", campaign)
		}

		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM campaigns WHERE organization_id = $1 AND name = $2`,
			f.org, name).Scan(&count); err != nil {
			t.Fatalf("count invalid campaign rows: %v", err)
		}
		if count != 0 {
			t.Fatalf("invalid campaign rows = %d, want 0", count)
		}
	})
}
