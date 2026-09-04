package sequence

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *sequenceService) Create(ctx context.Context, userID, campaignID string) (*models.Sequence, *errx.Error) {
	return s.sequenceRepository.Create(ctx, userID, campaignID)
}

func (s *sequenceService) Get(ctx context.Context, userID, campaignID string) ([]models.Sequence, *errx.Error) {
	return s.sequenceRepository.Get(ctx, userID, campaignID)
}

func (s *sequenceService) Update(ctx context.Context, userID, campaignID, sequenceID string, data *models.UpdateSequence) (*models.Sequence, *errx.Error) {
	// Branch routing is resolved (and made safe against deleted/dangling targets
	// and loops) at schedule time in the repository's finder; the repository also
	// validates branch shape before persisting. No cross-step write validation is
	// needed here — the canvas only ever points a branch at a real step or stop.
	return s.sequenceRepository.Update(ctx, userID, campaignID, sequenceID, data)
}

// UpdateLayout persists only step canvas coordinates (drag-to-stick). Cosmetic
// and high-churn, so it stays out of the audited content-update path.
func (s *sequenceService) UpdateLayout(ctx context.Context, userID, campaignID string, positions []models.SequencePosition) *errx.Error {
	return s.sequenceRepository.UpdateLayout(ctx, userID, campaignID, positions)
}

// Delete removes a step. Its attachment rows go with it through the cascade,
// so the objects behind them are listed first and dropped once the delete has
// committed — otherwise the bytes stay in storage against the org's quota with
// no row left to reach them.
func (s *sequenceService) Delete(ctx context.Context, userID, campaignID, sequenceID string) *errx.Error {
	keys := s.stepObjectKeys(ctx, campaignID, sequenceID)

	if xerr := s.sequenceRepository.Delete(ctx, userID, campaignID, sequenceID); xerr != nil {
		return xerr
	}

	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			sentry.CaptureException(fmt.Errorf("sequence %s delete: object %s: %w", sequenceID, key, err))
		}
	}
	return nil
}

// stepObjectKeys lists the storage keys of the files scoped to one step. Best
// effort: a step still deletes when they cannot be read, it just leaves its
// objects behind rather than refusing the edit.
func (s *sequenceService) stepObjectKeys(ctx context.Context, campaignID, sequenceID string) []string {
	if s.attachmentRepo == nil || s.storage == nil {
		return nil
	}
	cID, err := uuid.Parse(campaignID)
	if err != nil {
		return nil
	}
	sID, err := uuid.Parse(sequenceID)
	if err != nil {
		return nil
	}
	atts, err := s.attachmentRepo.ListForStep(ctx, cID, sID)
	if err != nil {
		sentry.CaptureException(fmt.Errorf("sequence %s delete: list attachments: %w", sequenceID, err))
		return nil
	}
	keys := make([]string, 0, len(atts))
	for _, a := range atts {
		// ListForStep also returns the campaign-wide files, which outlive the step.
		if a.SequenceID != nil && *a.SequenceID == sID {
			keys = append(keys, a.S3Key)
		}
	}
	return keys
}
