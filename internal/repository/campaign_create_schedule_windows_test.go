package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestCreateCampaignRejectsInvalidScheduleWindows(t *testing.T) {
	requireBadRequest := func(t *testing.T, got *errx.Error) {
		t.Helper()
		if got == nil {
			t.Fatal("Create() error = nil, want *errx.Error")
		}
		if got.Code != errx.BadRequest {
			t.Fatalf("Create() error code = %v, want %v", got.Code, errx.BadRequest)
		}
	}

	nineIntervals := make([]models.TimeInterval, 9)
	for i := range nineIntervals {
		nineIntervals[i] = models.TimeInterval{Start: i * 10, End: i*10 + 5}
	}

	tests := []struct {
		name    string
		windows models.ScheduleWindows
	}{
		{
			name: "end before start",
			windows: models.ScheduleWindows{
				1: []models.TimeInterval{{Start: 600, End: 540}},
			},
		},
		{
			name: "more than eight intervals",
			windows: models.ScheduleWindows{
				1: nineIntervals,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgID := uuid.New()
			_, got := (&campaignRepository{}).Create(context.Background(), "u", &orgID, &models.CreateCampaign{
				Name:            "x",
				ScheduleWindows: &tt.windows,
			})
			requireBadRequest(t, got)
		})
	}
}
