package tasks

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailPreviewInput is one step's templates plus the context the send path
// would have: the contact to render for, the campaign (opt-out footer,
// attachments, plain-text rule) and the sending mailbox (signature, From).
// Campaign and Account are optional; without them the preview is templates only.
type EmailPreviewInput struct {
	Subject   string
	BodyHTML  string
	BodyPlain string
	Contact   models.Contact
	Campaign  *models.Campaign
	Account   *models.Email
}

// EmailPreviewFrom is the sender as the recipient will see it.
type EmailPreviewFrom struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// EmailPreviewAttachment is an attachment the send would carry, metadata only.
type EmailPreviewAttachment struct {
	ID       uuid.UUID `json:"id"`
	Filename string    `json:"filename"`
	Size     int64     `json:"size"`
	MimeType string    `json:"mime_type"`
}

// EmailPreview is the rendered message with everything the send path adds
// after the template: signature, opt-out footer, sender and attachments.
type EmailPreview struct {
	TemplatePreview
	From        *EmailPreviewFrom        `json:"from,omitempty"`
	Attachments []EmailPreviewAttachment `json:"attachments,omitempty"`
}

// PreviewEmail renders a step the way the send path assembles it for one
// contact: template and spintax, then the plain-text rule, the mailbox
// signature and the opt-out footer, in send order. Tracking is left out since
// it only rewrites URLs. The opt-out link names no contact, so it can never
// suppress anyone if clicked.
func (s *tasksService) PreviewEmail(ctx context.Context, orgID uuid.UUID, in EmailPreviewInput) *EmailPreview {
	unsubURL := PreviewUnsubscribeLink
	var optOut *models.UnsubscribeSettings
	textOnly := false
	if in.Campaign != nil {
		if s.unsubLinks != nil && s.unsubLinks.Enabled() {
			unsubURL = s.unsubLinks.URL(orgID, in.Campaign.ID, uuid.Nil, time.Now())
		}
		settings := s.resolveOptOut(ctx, orgID, in.Campaign)
		optOut = &settings
		textOnly = in.Campaign.TextOnly
	}

	out := &EmailPreview{TemplatePreview: previewTemplatesWith(in.Subject, in.BodyHTML, in.BodyPlain, in.Contact, unsubURL)}
	out.BodyHTML, out.BodyPlain = finishBody(out.BodyHTML, out.BodyPlain, textOnly, in.Account, optOut, unsubURL)

	if in.Account != nil {
		out.From = &EmailPreviewFrom{Name: strings.TrimSpace(in.Account.Name), Email: in.Account.Email}
	}
	if in.Campaign != nil && s.attachmentRepo != nil {
		atts, err := s.attachmentRepo.ListByCampaign(ctx, in.Campaign.ID)
		if err != nil {
			log.Warn().Err(err).Str("campaign_id", in.Campaign.ID.String()).Msg("preview: load campaign attachments failed")
		}
		for _, a := range atts {
			out.Attachments = append(out.Attachments, EmailPreviewAttachment{ID: a.ID, Filename: a.Filename, Size: a.Size, MimeType: a.MimeType})
		}
	}
	return out
}

// finishBody applies what the send path adds after rendering, in its order:
// derive the plain part, drop HTML for a plain-text campaign, add the mailbox
// signature, then the opt-out footer (nil settings skip it). Shared by the
// preview and the test send so both show what a recipient gets.
func finishBody(bodyHTML, bodyPlain string, textOnly bool, account *models.Email, optOut *models.UnsubscribeSettings, unsubURL string) (string, string) {
	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}
	if textOnly {
		bodyHTML = ""
	}
	if account != nil && account.SignatureSync {
		if bodyHTML != "" {
			bodyHTML = AddSignature(bodyHTML, account.SignatureHTML, true)
		}
		if bodyPlain != "" {
			bodyPlain = AddSignature(bodyPlain, account.SignaturePlain, false)
		}
	}
	if optOut != nil {
		bodyHTML, bodyPlain = appendOptOut(bodyHTML, bodyPlain, *optOut, unsubURL)
	}
	return bodyHTML, bodyPlain
}

// campaignAttachmentRefs lists the files a campaign send carries, as the refs
// the worker resolves from object storage. A load failure sends without them
// rather than failing the send, and is logged.
func (s *tasksService) campaignAttachmentRefs(ctx context.Context, campaignID uuid.UUID) []models.AttachmentRef {
	if s.attachmentRepo == nil {
		return nil
	}
	atts, err := s.attachmentRepo.ListByCampaign(ctx, campaignID)
	if err != nil {
		log.Warn().Err(err).Str("campaign_id", campaignID.String()).Msg("Failed to load campaign attachments")
		return nil
	}
	refs := make([]models.AttachmentRef, 0, len(atts))
	for _, a := range atts {
		refs = append(refs, models.AttachmentRef{S3Key: a.S3Key, Filename: a.Filename, MimeType: a.MimeType})
	}
	return refs
}
