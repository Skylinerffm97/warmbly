package instancecheck

import (
	"context"
	"fmt"
)

const docsUpdates = "/development/updates/"

func updateChecks() []check {
	return []check{
		{id: "update_available", run: checkUpdateAvailable},
		{id: "updater_unreachable", run: checkUpdaterUnreachable},
	}
}

// checkUpdateAvailable is the health-page twin of the top-bar indicator, so an
// operator who only reads findings still learns a newer Warmbly exists.
func checkUpdateAvailable(ctx context.Context, d Deps, _ Input) *Finding {
	if d.Updates == nil {
		return nil
	}
	st := d.Updates.State(ctx, false)
	if !st.UpdateAvailable {
		return nil
	}
	var msg string
	switch {
	case st.Reason == "release" && st.Latest != nil:
		msg = fmt.Sprintf("Warmbly %s is available and this instance runs %s. ", st.Latest.Tag, st.Running.Version)
	case st.Updater.Checkout != nil:
		msg = fmt.Sprintf("The checkout is %d commits behind %s. ", st.Updater.Checkout.Behind, st.Updater.Checkout.Branch)
	default:
		msg = "A newer version is available. "
	}
	if st.Updater.Status == "ok" {
		msg += "Open the version pill in the top bar and choose Update and restart; the stack restarts and resumes on its own."
	} else if st.Updater.Release != nil {
		msg += "On the host, run docker compose pull && docker compose up -d in the install directory, or enable the updater to do it from this panel."
	} else {
		msg += "Run make upgrade on the host, or enable the updater to do it from this panel."
	}
	return result(CategoryUpdates, SeverityInfo, "An update is available", msg, docsUpdates)
}

// checkUpdaterUnreachable fires only when an updater is configured and not
// answering; an instance that never configured one is not misconfigured.
func checkUpdaterUnreachable(ctx context.Context, d Deps, _ Input) *Finding {
	if d.Updates == nil {
		return nil
	}
	st := d.Updates.State(ctx, false)
	if st.Updater.Status != "unreachable" {
		return nil
	}
	return result(CategoryUpdates, SeverityWarning, "The updater is not answering",
		"UPDATER_URL is set but the backend cannot reach the updater, so Update and restart in the panel cannot work. "+
			st.Updater.Error+" Remove UPDATER_URL if you update by hand.",
		docsUpdates)
}
