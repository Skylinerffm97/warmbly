package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// hasAccess is middleware.RequireAccess as an inline check, for a permission a
// handler needs only when an optional request field is present.
func (h *Handler) hasAccess(c *gin.Context, orgPerm models.OrganizationPermission, apiPerm uint64) *errx.Error {
	switch middleware.GetAuthType(c) {
	case middleware.AuthTypeAPIKey, middleware.AuthTypeOAuth:
		if !models.HasAPIPermission(middleware.GetAPIKeyPermissions(c), apiPerm) {
			return errx.New(errx.Forbidden, "insufficient API key permissions")
		}
		return nil
	default:
		if h.OrganizationService == nil {
			return nil
		}
		userID, err := middleware.GetUserUUID(c)
		if err != nil {
			return errx.ErrUnauthorized
		}
		orgID := middleware.GetOrganizationID(c)
		if orgID == nil {
			return errx.ErrNoOrganization
		}
		has, xerr := h.OrganizationService.HasPermission(c.Request.Context(), *orgID, userID, orgPerm)
		if xerr != nil {
			return xerr
		}
		if !has {
			return errx.ErrForbidden
		}
		return nil
	}
}

// mailboxAllowed enforces an API key's mailbox allow-list on an account id
// taken from a request body, the way RequireAPIKeyEmailAccountParam does for
// a route parameter.
func mailboxAllowed(c *gin.Context, accountID uuid.UUID) *errx.Error {
	if !middleware.APIKeyAllowsEmailAccount(c, accountID) {
		return errx.New(errx.Forbidden, "email account is not allowed for this API key")
	}
	return nil
}
