// Non-component helpers for the step preview's context (who it renders for,
// which mailbox signs it). Kept apart from PreviewControls.tsx so that file
// exports only components and stays fast-refresh friendly.

import type Contact from "@/lib/api/models/app/contacts/Contact";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import useCampaignSenders from "@/lib/api/hooks/app/campaigns/useCampaignSenders";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { SAMPLE } from "@/lib/templateVars";

export const SAMPLE_CONTACT_LABEL = `${SAMPLE.FirstName} ${SAMPLE.LastName} (sample)`;

export function contactLabel(c: Contact): string {
    const name = `${c.first_name ?? ""} ${c.last_name ?? ""}`.trim();
    return name || c.email;
}

// Resolves the campaign's sending pool to mailbox rows, so pickers can label
// a sender by address. Only enabled senders are offered: a paused one never
// sends this step.
export function useCampaignSenderInboxes(campaignId: string): { inboxes: Inbox[]; loading: boolean } {
    const senders = useCampaignSenders(campaignId, !!campaignId);
    const emails = useEmails({ query: "", tag: "", limit: 200, enabled: !!campaignId });
    const ids = new Set((senders.data ?? []).filter((s) => s.enabled).map((s) => s.email_account_id));
    const inboxes = emails.emails.filter((e) => ids.has(e.id));
    return { inboxes, loading: senders.isLoading || emails.isLoading };
}
