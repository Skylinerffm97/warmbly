package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/app/instancesettings"
)

// RetentionSource is the operator-editable retention section, satisfied by
// instancesettings.Service. The retention jobs read it on every pass rather
// than capturing a window at construction, so shortening one in the admin
// panel takes effect on the next sweep instead of at the next restart.
type RetentionSource interface {
	RetentionWindows(ctx context.Context) instancesettings.Retention
}
