// How a campaign's linked segments relate to it right now, shared by the
// Leads tab strip, its empty state and the link dialog's toast.

import type { CampaignSegmentLink } from "@/lib/api/models/app/segments/Segment";

export function linkSummary(l: CampaignSegmentLink): string {
    if (l.contact_count === 0) return "Matches no contacts yet";
    const members = `${l.contact_count.toLocaleString()} member${l.contact_count === 1 ? "" : "s"}`;
    const held =
        l.held_out_count > 0
            ? `, ${l.held_out_count.toLocaleString()} removed by hand and held out`
            : "";
    if (l.lead_count === l.contact_count) return `${members}, all leads${held}`;
    return `${members}, ${l.lead_count.toLocaleString()} of them leads${held}`;
}

export function linkTotals(links: CampaignSegmentLink[]): { members: number; held: number } {
    return {
        members: links.reduce((n, l) => n + l.contact_count, 0),
        held: links.reduce((n, l) => n + l.held_out_count, 0),
    };
}

// Why linked segments added no leads: empty, held out, or nothing yet.
export function linksEmptyReason(links: CampaignSegmentLink[]): string {
    const names = links.map((l) => l.name).join(", ");
    const { members, held } = linkTotals(links);
    if (members === 0) {
        return `${names} ${links.length === 1 ? "matches" : "match"} no contacts right now. Leads join automatically as contacts enter the segment.`;
    }
    if (held > 0) {
        return `${held.toLocaleString()} member${held === 1 ? "" : "s"} of ${names} ${held === 1 ? "was" : "were"} removed from this campaign by hand, so automatic enrolment leaves them out until you add them back.`;
    }
    return `${links.map(linkSummary).join("; ")}. Members are enrolled automatically within a couple of minutes.`;
}
