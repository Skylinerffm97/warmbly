package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type SegmentRepository interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.Segment, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, seg *models.Segment) (*models.Segment, *errx.Error)
	Update(ctx context.Context, orgID uuid.UUID, seg *models.Segment) (*models.Segment, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	// ReferencedBy names the segments whose conditions point at id.
	ReferencedBy(ctx context.Context, orgID, id uuid.UUID) ([]string, *errx.Error)
	// Count evaluates a definition (saved or not) against the org's contacts.
	Count(ctx context.Context, orgID uuid.UUID, id *uuid.UUID, match models.SegmentMatch, conds []models.SegmentCondition) (int, *errx.Error)
	// SetMembers writes a manual override for each contact; Auto removes it.
	SetMembers(ctx context.Context, orgID, segmentID uuid.UUID, contactIDs []uuid.UUID, mode models.SegmentMemberMode) (int, *errx.Error)
	// MemberModes reports the manual override of each listed contact.
	MemberModes(ctx context.Context, segmentID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]models.SegmentMemberMode, *errx.Error)
	// AddToCampaign enrols every current member of the segment as a lead.
	AddToCampaign(ctx context.Context, orgID uuid.UUID, actor string, segmentID, campaignID uuid.UUID) (*models.SegmentAddToCampaignResult, *errx.Error)
	// ListForCampaign lists the segments linked to a campaign.
	ListForCampaign(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.CampaignSegmentLink, *errx.Error)
	// SetForCampaign replaces the campaign's linked segments.
	SetForCampaign(ctx context.Context, orgID, campaignID uuid.UUID, segmentIDs []uuid.UUID) *errx.Error
	// ReplaceForCampaign replaces the links and enrols the members in one
	// transaction, so a failed enrolment leaves no half-applied link set.
	// Returns how many leads were new and the campaign's status.
	ReplaceForCampaign(ctx context.Context, orgID, campaignID uuid.UUID, segmentIDs []uuid.UUID) (int, string, *errx.Error)
	// SyncCampaignSegments enrols every current member of the campaign's
	// linked segments that is not yet a lead; returns how many were added.
	SyncCampaignSegments(ctx context.Context, orgID, campaignID uuid.UUID) (int, *errx.Error)
	// LinkedCampaigns lists campaigns with linked segments; nil orgID sweeps
	// the whole instance.
	LinkedCampaigns(ctx context.Context, orgID *uuid.UUID) ([]models.LinkedCampaign, *errx.Error)
	// LinkedCampaignsForSegments lists the campaigns any of the segments link to.
	LinkedCampaignsForSegments(ctx context.Context, orgID uuid.UUID, segmentIDs []uuid.UUID) ([]models.LinkedCampaign, *errx.Error)
	// CampaignsUsingSegment names the campaigns a segment is linked to.
	CampaignsUsingSegment(ctx context.Context, orgID, segmentID uuid.UUID) ([]string, *errx.Error)
	// SegmentsForContact evaluates every segment of the org for one contact.
	SegmentsForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactSegment, *errx.Error)
	// ListOverrides lists the manually included and excluded contacts.
	ListOverrides(ctx context.Context, orgID, segmentID uuid.UUID) ([]models.SegmentOverride, *errx.Error)
}

type segmentRepository struct {
	DB *db.DB
}

func NewSegmentRepository(d *db.DB) SegmentRepository {
	return &segmentRepository{DB: d}
}

const segmentColumns = `s.id, s.organization_id, s.created_by, s.name, s.description, s.color, s.match, s.conditions,
	(SELECT COUNT(*) FROM segment_members sm WHERE sm.segment_id = s.id AND sm.mode = 'include'),
	(SELECT COUNT(*) FROM segment_members sm WHERE sm.segment_id = s.id AND sm.mode = 'exclude'),
	s.created_at, s.updated_at`

func scanSegment(row pgx.Row) (*models.Segment, error) {
	var s models.Segment
	var raw []byte
	if err := row.Scan(&s.ID, &s.OrganizationID, &s.CreatedBy, &s.Name, &s.Description, &s.Color, &s.Match, &raw,
		&s.IncludedCount, &s.ExcludedCount, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Conditions = []models.SegmentCondition{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s.Conditions); err != nil {
			return nil, err
		}
	}
	return &s, nil
}

func (r *segmentRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.Segment, *errx.Error) {
	rows, err := r.DB.Query(ctx, `SELECT `+segmentColumns+` FROM segments s WHERE s.organization_id = $1 ORDER BY lower(s.name) ASC`, orgID)
	if err != nil {
		db.CaptureError(err, "segments list", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.Segment{}
	for rows.Next() {
		s, err := scanSegment(rows)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, *s)
	}
	for i := range out {
		n, xerr := r.Count(ctx, orgID, &out[i].ID, out[i].Match, out[i].Conditions)
		if xerr != nil {
			return nil, xerr
		}
		out[i].ContactCount = n
	}
	return out, nil
}

func (r *segmentRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error) {
	s, err := scanSegment(r.DB.QueryRow(ctx, `SELECT `+segmentColumns+` FROM segments s WHERE s.organization_id = $1 AND s.id = $2`, orgID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.New(errx.NotFound, "segment not found")
		}
		db.CaptureError(err, "segments get", nil, "queryrow")
		return nil, errx.InternalError()
	}
	n, xerr := r.Count(ctx, orgID, &s.ID, s.Match, s.Conditions)
	if xerr != nil {
		return nil, xerr
	}
	s.ContactCount = n
	return s, nil
}

func (r *segmentRepository) Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, seg *models.Segment) (*models.Segment, *errx.Error) {
	var total int
	if err := r.DB.QueryRow(ctx, `SELECT COUNT(*) FROM segments WHERE organization_id = $1`, orgID).Scan(&total); err != nil {
		db.CaptureError(err, "segments count", nil, "queryrow")
		return nil, errx.InternalError()
	}
	if total >= models.SegmentsPerOrgMax {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("a workspace can have at most %d segments", models.SegmentsPerOrgMax))
	}
	conds, _ := json.Marshal(seg.Conditions)
	var id uuid.UUID
	err := r.DB.QueryRow(ctx, `
		INSERT INTO segments (organization_id, created_by, name, description, color, match, conditions)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`, orgID, createdBy, seg.Name, seg.Description, seg.Color, seg.Match, conds).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errx.New(errx.Conflict, "a segment with that name already exists")
		}
		db.CaptureError(err, "segments insert", nil, "queryrow")
		return nil, errx.InternalError()
	}
	return r.Get(ctx, orgID, id)
}

func (r *segmentRepository) Update(ctx context.Context, orgID uuid.UUID, seg *models.Segment) (*models.Segment, *errx.Error) {
	conds, _ := json.Marshal(seg.Conditions)
	tag, err := r.DB.Exec(ctx, `
		UPDATE segments SET name = $3, description = $4, color = $5, match = $6, conditions = $7, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, seg.ID, seg.Name, seg.Description, seg.Color, seg.Match, conds)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errx.New(errx.Conflict, "a segment with that name already exists")
		}
		db.CaptureError(err, "segments update", nil, "exec")
		return nil, errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return nil, errx.New(errx.NotFound, "segment not found")
	}
	return r.Get(ctx, orgID, seg.ID)
}

func (r *segmentRepository) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	tag, err := r.DB.Exec(ctx, `DELETE FROM segments WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		db.CaptureError(err, "segments delete", nil, "exec")
		return errx.InternalError()
	}
	if tag.RowsAffected() == 0 {
		return errx.New(errx.NotFound, "segment not found")
	}
	return nil
}

func (r *segmentRepository) ReferencedBy(ctx context.Context, orgID, id uuid.UUID) ([]string, *errx.Error) {
	needle, _ := json.Marshal([]map[string]any{{"field": "segment", "values": []string{id.String()}}})
	rows, err := r.DB.Query(ctx, `SELECT name FROM segments WHERE organization_id = $1 AND id <> $2 AND conditions @> $3::jsonb ORDER BY lower(name)`, orgID, id, needle)
	if err != nil {
		db.CaptureError(err, "segments referenced", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		names = append(names, n)
	}
	return names, nil
}

func (r *segmentRepository) Count(ctx context.Context, orgID uuid.UUID, id *uuid.UUID, match models.SegmentMatch, conds []models.SegmentCondition) (int, *errx.Error) {
	def := &segmentDef{Match: match, Conditions: conds}
	if id != nil {
		def.ID = *id
	}
	args := []any{orgID}
	clause, args, err := compileSegment(ctx, r.DB, orgID, def, args)
	if err != nil {
		db.CaptureError(err, "segment compile", nil, "query")
		return 0, errx.InternalError()
	}
	query := `SELECT COUNT(*) FROM contacts c WHERE c.organization_id = $1 AND (` + clause + `)`
	var n int
	if err := r.DB.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		db.CaptureError(err, query, args, "queryrow")
		return 0, errx.InternalError()
	}
	return n, nil
}

func (r *segmentRepository) SetMembers(ctx context.Context, orgID, segmentID uuid.UUID, contactIDs []uuid.UUID, mode models.SegmentMemberMode) (int, *errx.Error) {
	var tag pgconn.CommandTag
	var err error
	if mode == models.SegmentMemberAuto {
		tag, err = r.DB.Exec(ctx, `
			DELETE FROM segment_members sm USING segments s
			WHERE sm.segment_id = s.id AND s.organization_id = $1 AND s.id = $2 AND sm.contact_id = ANY($3::uuid[])`,
			orgID, segmentID, contactIDs)
	} else {
		tag, err = r.DB.Exec(ctx, `
			INSERT INTO segment_members (segment_id, contact_id, mode)
			SELECT s.id, c.id, $4
			FROM segments s
			JOIN contacts c ON c.organization_id = s.organization_id
			WHERE s.organization_id = $1 AND s.id = $2 AND c.id = ANY($3::uuid[])
			ON CONFLICT (segment_id, contact_id) DO UPDATE SET mode = EXCLUDED.mode, created_at = now()`,
			orgID, segmentID, contactIDs, string(mode))
	}
	if err != nil {
		db.CaptureError(err, "segment members", nil, "exec")
		return 0, errx.InternalError()
	}
	return int(tag.RowsAffected()), nil
}

func (r *segmentRepository) MemberModes(ctx context.Context, segmentID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]models.SegmentMemberMode, *errx.Error) {
	out := map[uuid.UUID]models.SegmentMemberMode{}
	if len(contactIDs) == 0 {
		return out, nil
	}
	rows, err := r.DB.Query(ctx, `SELECT contact_id, mode FROM segment_members WHERE segment_id = $1 AND contact_id = ANY($2::uuid[])`, segmentID, contactIDs)
	if err != nil {
		db.CaptureError(err, "segment member modes", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var mode string
		if err := rows.Scan(&id, &mode); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		out[id] = models.SegmentMemberMode(mode)
	}
	return out, nil
}

func (r *segmentRepository) AddToCampaign(ctx context.Context, orgID uuid.UUID, actor string, segmentID, campaignID uuid.UUID) (*models.SegmentAddToCampaignResult, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1 AND organization_id = $2`, campaignID, orgID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.New(errx.NotFound, "campaign not found")
		}
		db.CaptureError(err, "campaign status", nil, "queryrow")
		return nil, errx.InternalError()
	}

	args := []any{orgID}
	clause, args, err := compileSavedSegment(ctx, tx, orgID, segmentID, args)
	if err != nil {
		db.CaptureError(err, "segment compile", nil, "query")
		return nil, errx.InternalError()
	}
	if clause == "FALSE" {
		return nil, errx.New(errx.NotFound, "segment not found")
	}

	var members int
	countQ := `SELECT COUNT(*) FROM contacts c WHERE c.organization_id = $1 AND (` + clause + `)`
	if err := tx.QueryRow(ctx, countQ, args...).Scan(&members); err != nil {
		db.CaptureError(err, countQ, args, "queryrow")
		return nil, errx.InternalError()
	}

	links, err := insertSegmentLeads(ctx, tx, orgID, actorID(actor), clause, args, campaignID, false)
	if err != nil {
		db.CaptureError(err, "segment enrol", nil, "query")
		return nil, errx.InternalError()
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}
	return &models.SegmentAddToCampaignResult{CampaignID: campaignID, Added: len(links), Members: members, Status: status}, nil
}

// insertSegmentLeads enrols every contact matching the precompiled segment
// clause as a lead, logging a campaign_added activity for each row that was
// actually new. The campaign is bound after the clause's own parameters.
//
// respectRemovals decides what a manual "remove from campaign" means here:
// the automatic sync honours the removal record and skips the pair, while an
// explicit enrol (the one-shot add-to-campaign) clears it and re-adds.
func insertSegmentLeads(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, actor *uuid.UUID, clause string, args []any, campaignID uuid.UUID, respectRemovals bool) ([]contactLink, error) {
	args = append(args, campaignID)
	guard := ""
	if respectRemovals {
		guard = fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM campaign_lead_removals r WHERE r.campaign_id = $%d AND r.contact_id = c.id)`, len(args))
	} else {
		clearQ := fmt.Sprintf(`DELETE FROM campaign_lead_removals r
			WHERE r.campaign_id = $%d AND r.contact_id IN (SELECT c.id FROM contacts c WHERE c.organization_id = $1 AND (%s))`, len(args), clause)
		if _, err := tx.Exec(ctx, clearQ, args...); err != nil {
			return nil, err
		}
	}
	insertQ := fmt.Sprintf(`INSERT INTO campaign_leads (contact_id, campaign_id)
		SELECT c.id, $%d::uuid FROM contacts c WHERE c.organization_id = $1 AND (%s)%s
		ON CONFLICT DO NOTHING
		RETURNING contact_id, campaign_id`, len(args), clause, guard)
	rows, err := tx.Query(ctx, insertQ, args...)
	if err != nil {
		return nil, err
	}
	links, err := collectLinkPairs(rows)
	if err != nil {
		return nil, err
	}
	if err := logCampaignLinks(ctx, tx, orgID, actor, models.ActivityCampaignAdded, links); err != nil {
		return nil, err
	}
	return links, nil
}

func (r *segmentRepository) ListForCampaign(ctx context.Context, orgID, campaignID uuid.UUID) ([]models.CampaignSegmentLink, *errx.Error) {
	var exists bool
	if err := r.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM campaigns WHERE id = $1 AND organization_id = $2)`, campaignID, orgID).Scan(&exists); err != nil {
		db.CaptureError(err, "campaign exists", nil, "queryrow")
		return nil, errx.InternalError()
	}
	if !exists {
		return nil, errx.New(errx.NotFound, "campaign not found")
	}
	rows, err := r.DB.Query(ctx, `
		SELECT s.id, s.name, s.color, s.description, cs.created_at
		FROM campaign_segments cs
		JOIN segments s ON s.id = cs.segment_id
		WHERE cs.campaign_id = $1
		ORDER BY lower(s.name) ASC`, campaignID)
	if err != nil {
		db.CaptureError(err, "campaign segments list", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.CampaignSegmentLink{}
	for rows.Next() {
		var l models.CampaignSegmentLink
		if err := rows.Scan(&l.SegmentID, &l.Name, &l.Color, &l.Description, &l.LinkedAt); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, l)
	}
	// A mid-stream read failure ends Next() early with no scan error; without
	// this the Leads tab would render a truncated link list as the truth.
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "campaign segments list", nil, "rows")
		return nil, errx.InternalError()
	}
	if len(out) == 0 {
		return out, nil
	}
	if xerr := r.campaignLinkCounts(ctx, orgID, campaignID, out); xerr != nil {
		return nil, xerr
	}
	return out, nil
}

// campaignLinkCounts fills the live counts of every link in one contacts scan:
// members, members that are leads, and members held out (a manual removal and
// not a lead, exactly the pairs the sync skips).
func (r *segmentRepository) campaignLinkCounts(ctx context.Context, orgID, campaignID uuid.UUID, links []models.CampaignSegmentLink) *errx.Error {
	roots := make([]uuid.UUID, len(links))
	for i := range links {
		roots[i] = links[i].SegmentID
	}
	graph, err := loadSegmentGraph(ctx, r.DB, orgID, roots)
	if err != nil {
		db.CaptureError(err, "segment compile", nil, "query")
		return errx.InternalError()
	}
	b := &segmentBuilder{orgID: orgID, args: []any{orgID}, graph: graph}
	clauses := make([]string, len(links))
	for i := range links {
		// A segment deleted between the two reads compiles to FALSE; its
		// link row cascades away with it.
		clauses[i] = "FALSE"
		if def, ok := graph[links[i].SegmentID]; ok {
			clauses[i] = b.segmentClause(def, true, map[uuid.UUID]bool{})
		}
	}
	args := append(b.args, campaignID)
	cp := fmt.Sprintf("$%d", len(args))
	cols := make([]string, 0, len(links)*3)
	for _, cl := range clauses {
		cols = append(cols,
			`COUNT(*) FILTER (WHERE (`+cl+`))`,
			`COUNT(cl.contact_id) FILTER (WHERE (`+cl+`))`,
			`COUNT(lr.contact_id) FILTER (WHERE cl.contact_id IS NULL AND (`+cl+`))`)
	}
	query := `SELECT ` + strings.Join(cols, ", ") + `
		FROM contacts c
		LEFT JOIN campaign_leads cl ON cl.campaign_id = ` + cp + ` AND cl.contact_id = c.id
		LEFT JOIN campaign_lead_removals lr ON lr.campaign_id = ` + cp + ` AND lr.contact_id = c.id
		WHERE c.organization_id = $1 AND ((` + strings.Join(clauses, ") OR (") + `))`
	dest := make([]any, 0, len(links)*3)
	for i := range links {
		dest = append(dest, &links[i].ContactCount, &links[i].LeadCount, &links[i].HeldOutCount)
	}
	if err := r.DB.QueryRow(ctx, query, args...).Scan(dest...); err != nil {
		db.CaptureError(err, query, args, "queryrow")
		return errx.InternalError()
	}
	return nil
}

func (r *segmentRepository) SetForCampaign(ctx context.Context, orgID, campaignID uuid.UUID, segmentIDs []uuid.UUID) *errx.Error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)
	if _, xerr := setForCampaignTx(ctx, tx, orgID, campaignID, segmentIDs); xerr != nil {
		return xerr
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return errx.InternalError()
	}
	return nil
}

func (r *segmentRepository) ReplaceForCampaign(ctx context.Context, orgID, campaignID uuid.UUID, segmentIDs []uuid.UUID) (int, string, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return 0, "", errx.InternalError()
	}
	defer tx.Rollback(ctx)
	status, xerr := setForCampaignTx(ctx, tx, orgID, campaignID, segmentIDs)
	if xerr != nil {
		return 0, "", xerr
	}
	added, xerr := syncCampaignSegmentsTx(ctx, tx, orgID, campaignID)
	if xerr != nil {
		return 0, "", xerr
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return 0, "", errx.InternalError()
	}
	return added, status, nil
}

// setForCampaignTx replaces the links under a lock on the campaign row, so two
// concurrent replacements cannot commit the union of their sets; the status
// read under that lock is what the caller reacts to.
func setForCampaignTx(ctx context.Context, tx pgx.Tx, orgID, campaignID uuid.UUID, segmentIDs []uuid.UUID) (string, *errx.Error) {
	// A nil slice would reach Postgres as ANY(NULL) and skip the delete.
	if segmentIDs == nil {
		segmentIDs = []uuid.UUID{}
	}
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1 AND organization_id = $2 FOR UPDATE`, campaignID, orgID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errx.New(errx.NotFound, "campaign not found")
		}
		db.CaptureError(err, "campaign lock", nil, "queryrow")
		return "", errx.InternalError()
	}
	if len(segmentIDs) > 0 {
		var n int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM segments WHERE organization_id = $1 AND id = ANY($2::uuid[])`, orgID, segmentIDs).Scan(&n); err != nil {
			db.CaptureError(err, "segments verify", nil, "queryrow")
			return "", errx.InternalError()
		}
		if n != len(segmentIDs) {
			return "", errx.New(errx.BadRequest, "a linked segment does not exist")
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM campaign_segments WHERE campaign_id = $1 AND NOT (segment_id = ANY($2::uuid[]))`, campaignID, segmentIDs); err != nil {
		db.CaptureError(err, "campaign segments delete", nil, "exec")
		return "", errx.InternalError()
	}
	if len(segmentIDs) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO campaign_segments (campaign_id, segment_id) SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, campaignID, segmentIDs); err != nil {
			db.CaptureError(err, "campaign segments insert", nil, "exec")
			return "", errx.InternalError()
		}
	}
	return status, nil
}

func (r *segmentRepository) SyncCampaignSegments(ctx context.Context, orgID, campaignID uuid.UUID) (int, *errx.Error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return 0, errx.InternalError()
	}
	defer tx.Rollback(ctx)
	added, xerr := syncCampaignSegmentsTx(ctx, tx, orgID, campaignID)
	if xerr != nil {
		return 0, xerr
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return 0, errx.InternalError()
	}
	return added, nil
}

func syncCampaignSegmentsTx(ctx context.Context, tx pgx.Tx, orgID, campaignID uuid.UUID) (int, *errx.Error) {
	rows, err := tx.Query(ctx, `
		SELECT cs.segment_id FROM campaign_segments cs
		JOIN campaigns cp ON cp.id = cs.campaign_id
		WHERE cp.id = $1 AND cp.organization_id = $2`, campaignID, orgID)
	if err != nil {
		db.CaptureError(err, "campaign segments sync", nil, "query")
		return 0, errx.InternalError()
	}
	var segmentIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			db.CaptureError(err, "", nil, "scan")
			return 0, errx.InternalError()
		}
		segmentIDs = append(segmentIDs, id)
	}
	rows.Close()
	// A truncated id list here would enrol part of the audience and still
	// commit as a success, so a read failure has to abort the sync.
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "campaign segments sync", nil, "rows")
		return 0, errx.InternalError()
	}

	total := 0
	for _, segID := range segmentIDs {
		args := []any{orgID}
		clause, args, cerr := compileSavedSegment(ctx, tx, orgID, segID, args)
		if cerr != nil {
			db.CaptureError(cerr, "segment compile", nil, "query")
			return 0, errx.InternalError()
		}
		// FALSE means the segment vanished mid-pass; the link row follows it.
		if clause == "FALSE" {
			continue
		}
		links, lerr := insertSegmentLeads(ctx, tx, orgID, nil, clause, args, campaignID, true)
		if lerr != nil {
			db.CaptureError(lerr, "segment enrol", nil, "query")
			return 0, errx.InternalError()
		}
		total += len(links)
	}
	return total, nil
}

const linkedCampaignsQuery = `
	SELECT DISTINCT cs.campaign_id, cp.organization_id, cp.status
	FROM campaign_segments cs
	JOIN campaigns cp ON cp.id = cs.campaign_id
	WHERE cp.organization_id IS NOT NULL`

func scanLinkedCampaigns(rows pgx.Rows) ([]models.LinkedCampaign, *errx.Error) {
	defer rows.Close()
	out := []models.LinkedCampaign{}
	for rows.Next() {
		var l models.LinkedCampaign
		if err := rows.Scan(&l.CampaignID, &l.OrganizationID, &l.Status); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "linked campaigns", nil, "rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

func (r *segmentRepository) LinkedCampaigns(ctx context.Context, orgID *uuid.UUID) ([]models.LinkedCampaign, *errx.Error) {
	q := linkedCampaignsQuery
	args := []any{}
	if orgID != nil {
		q += ` AND cp.organization_id = $1`
		args = append(args, *orgID)
	}
	rows, err := r.DB.Query(ctx, q, args...)
	if err != nil {
		db.CaptureError(err, "linked campaigns", nil, "query")
		return nil, errx.InternalError()
	}
	return scanLinkedCampaigns(rows)
}

func (r *segmentRepository) LinkedCampaignsForSegments(ctx context.Context, orgID uuid.UUID, segmentIDs []uuid.UUID) ([]models.LinkedCampaign, *errx.Error) {
	if len(segmentIDs) == 0 {
		return []models.LinkedCampaign{}, nil
	}
	rows, err := r.DB.Query(ctx, linkedCampaignsQuery+` AND cp.organization_id = $1 AND cs.segment_id = ANY($2::uuid[])`, orgID, segmentIDs)
	if err != nil {
		db.CaptureError(err, "linked campaigns for segments", nil, "query")
		return nil, errx.InternalError()
	}
	return scanLinkedCampaigns(rows)
}

func (r *segmentRepository) CampaignsUsingSegment(ctx context.Context, orgID, segmentID uuid.UUID) ([]string, *errx.Error) {
	rows, err := r.DB.Query(ctx, `
		SELECT cp.name FROM campaign_segments cs
		JOIN campaigns cp ON cp.id = cs.campaign_id
		WHERE cp.organization_id = $1 AND cs.segment_id = $2
		ORDER BY lower(cp.name)`, orgID, segmentID)
	if err != nil {
		db.CaptureError(err, "campaigns using segment", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "campaigns using segment", nil, "rows")
		return nil, errx.InternalError()
	}
	return names, nil
}

func (r *segmentRepository) SegmentsForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactSegment, *errx.Error) {
	rows, err := r.DB.Query(ctx, `SELECT `+segmentColumns+` FROM segments s WHERE s.organization_id = $1 ORDER BY lower(s.name) ASC`, orgID)
	if err != nil {
		db.CaptureError(err, "segments for contact", nil, "query")
		return nil, errx.InternalError()
	}
	var defs []*models.Segment
	for rows.Next() {
		seg, err := scanSegment(rows)
		if err != nil {
			rows.Close()
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		defs = append(defs, seg)
	}
	rows.Close()
	out := make([]models.ContactSegment, 0, len(defs))
	if len(defs) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
	}
	modes := map[uuid.UUID]models.SegmentMemberMode{}
	mrows, err := r.DB.Query(ctx, `SELECT segment_id, mode FROM segment_members WHERE contact_id = $1 AND segment_id = ANY($2::uuid[])`, contactID, ids)
	if err != nil {
		db.CaptureError(err, "contact segment modes", nil, "query")
		return nil, errx.InternalError()
	}
	for mrows.Next() {
		var id uuid.UUID
		var mode string
		if err := mrows.Scan(&id, &mode); err != nil {
			mrows.Close()
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		modes[id] = models.SegmentMemberMode(mode)
	}
	mrows.Close()
	// One membership probe per segment: the compiled predicate over a single
	// contact row, which is what the segment page would compute for it anyway.
	for _, d := range defs {
		args := []any{orgID, contactID}
		clause, args, cerr := compileSavedSegment(ctx, r.DB, orgID, d.ID, args)
		if cerr != nil {
			db.CaptureError(cerr, "segment compile", nil, "query")
			return nil, errx.InternalError()
		}
		var member bool
		q := `SELECT EXISTS (SELECT 1 FROM contacts c WHERE c.organization_id = $1 AND c.id = $2 AND (` + clause + `))`
		if err := r.DB.QueryRow(ctx, q, args...).Scan(&member); err != nil {
			db.CaptureError(err, q, args, "queryrow")
			return nil, errx.InternalError()
		}
		out = append(out, models.ContactSegment{ID: d.ID, Name: d.Name, Color: d.Color, Mode: modes[d.ID], Member: member})
	}
	return out, nil
}

func (r *segmentRepository) ListOverrides(ctx context.Context, orgID, segmentID uuid.UUID) ([]models.SegmentOverride, *errx.Error) {
	rows, err := r.DB.Query(ctx, `
		SELECT c.id, c.first_name, c.last_name, c.email, c.company, sm.mode, sm.created_at
		FROM segment_members sm
		JOIN segments s ON s.id = sm.segment_id
		JOIN contacts c ON c.id = sm.contact_id
		WHERE s.organization_id = $1 AND s.id = $2
		ORDER BY sm.mode ASC, sm.created_at DESC
		LIMIT $3`, orgID, segmentID, models.SegmentOverridesMax)
	if err != nil {
		db.CaptureError(err, "segment overrides", nil, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.SegmentOverride{}
	for rows.Next() {
		var o models.SegmentOverride
		var mode string
		if err := rows.Scan(&o.ContactID, &o.FirstName, &o.LastName, &o.Email, &o.Company, &mode, &o.CreatedAt); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		o.Mode = models.SegmentMemberMode(mode)
		out = append(out, o)
	}
	return out, nil
}
