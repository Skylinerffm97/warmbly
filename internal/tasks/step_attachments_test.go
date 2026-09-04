package tasks

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// stubAttachmentRepo records the scope a send asked for and answers with the
// rows a step-scoped query would return.
type stubAttachmentRepo struct {
	repository.AttachmentRepository
	gotCampaign uuid.UUID
	gotSequence uuid.UUID
	rows        []models.CampaignAttachment
}

func (s *stubAttachmentRepo) ListForStep(_ context.Context, campaignID, sequenceID uuid.UUID) ([]models.CampaignAttachment, error) {
	s.gotCampaign, s.gotSequence = campaignID, sequenceID
	return s.rows, nil
}

// The files a send carries are the step's, not the campaign's: sequence_id was
// stored on upload and honoured by the duplicate path, but the send path listed
// every attachment of the campaign, so a file attached to step 1 rode step 2,
// step 3 and every follow-up after them.
func TestCampaignAttachmentRefsAsksForTheStepsFiles(t *testing.T) {
	campaignID, sequenceID := uuid.New(), uuid.New()
	repo := &stubAttachmentRepo{rows: []models.CampaignAttachment{
		{S3Key: "k1", Filename: "brief.pdf", MimeType: "application/pdf"},
	}}
	s := &tasksService{attachmentRepo: repo}

	refs := s.campaignAttachmentRefs(context.Background(), campaignID, sequenceID)

	if repo.gotCampaign != campaignID || repo.gotSequence != sequenceID {
		t.Fatalf("listed (%s, %s), want the campaign and step being sent (%s, %s)",
			repo.gotCampaign, repo.gotSequence, campaignID, sequenceID)
	}
	if len(refs) != 1 || refs[0].S3Key != "k1" || refs[0].Filename != "brief.pdf" {
		t.Fatalf("refs = %#v, want the step's one file", refs)
	}
}

// Without an attachment store a send still goes out, carrying nothing.
func TestCampaignAttachmentRefsWithoutAStore(t *testing.T) {
	s := &tasksService{}
	if refs := s.campaignAttachmentRefs(context.Background(), uuid.New(), uuid.New()); refs != nil {
		t.Fatalf("refs = %#v, want none", refs)
	}
}
