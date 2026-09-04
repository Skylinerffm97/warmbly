// A campaign's linked segments on its Leads tab: one chip per segment with its
// live lead count, click to filter the list to it.

import { Link } from "react-router-dom";
import { ExternalLinkIcon, LayersIcon, Loader2Icon, UserPlusIcon } from "lucide-react";

import type { CampaignSegmentLink } from "@/lib/api/models/app/segments/Segment";
import { cn } from "@/lib/utils";
import { linkSummary } from "./linkedSegments";

export default function LinkedSegmentsStrip({
    links,
    activeSegmentId,
    onToggle,
    onManage,
    onReenrol,
    reenrolling,
}: {
    links: CampaignSegmentLink[];
    activeSegmentId?: string;
    onToggle: (segmentId: string) => void;
    onManage: () => void;
    // Adds the held-out members back (the one-shot enrol clears the removal
    // records); undefined hides the action, for members without the permission.
    onReenrol?: (link: CampaignSegmentLink) => void;
    reenrolling?: string | null;
}) {
    if (links.length === 0) return null;
    return (
        <div className="px-5 py-1.5 border-b border-slate-200/60 bg-slate-50/40 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 shrink-0">
            <span className="inline-flex items-center gap-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                <LayersIcon className="w-3 h-3" />
                Linked segments
            </span>
            <ul className="flex flex-wrap items-center gap-1.5 min-w-0">
                {links.map((l) => {
                    const active = activeSegmentId === l.segment_id;
                    const empty = l.contact_count === 0;
                    const held = l.held_out_count > 0;
                    return (
                        <li key={l.segment_id} className="inline-flex items-center">
                            <button
                                type="button"
                                onClick={() => onToggle(l.segment_id)}
                                aria-pressed={active}
                                title={`${linkSummary(l)}. Click to ${active ? "show every lead" : "show only these leads"}.`}
                                className={cn(
                                    "h-6 pl-1.5 pr-2 rounded-l-md border inline-flex items-center gap-1.5 text-[11.5px] transition-colors",
                                    active
                                        ? "border-sky-300 bg-sky-50 text-sky-800"
                                        : "border-slate-200 bg-white text-slate-700 hover:border-slate-300 hover:text-slate-900",
                                )}
                            >
                                <span className="size-2 rounded-full shrink-0" style={{ backgroundColor: l.color }} />
                                <span className="font-medium truncate max-w-[160px]">{l.name}</span>
                                <span
                                    className={cn(
                                        "font-mono text-[10.5px] tabular-nums",
                                        empty || held
                                            ? "text-amber-700"
                                            : active
                                              ? "text-sky-700"
                                              : "text-slate-500",
                                    )}
                                >
                                    {empty
                                        ? "no contacts"
                                        : `${l.lead_count.toLocaleString()}/${l.contact_count.toLocaleString()}`}
                                </span>
                                {held && (
                                    <span className="text-[10px] text-amber-700">
                                        {l.held_out_count.toLocaleString()} held out
                                    </span>
                                )}
                            </button>
                            <Link
                                to={`/app/contacts/segments/${l.segment_id}`}
                                aria-label={`Open the ${l.name} segment`}
                                title="Open segment"
                                className={cn(
                                    "h-6 px-1.5 rounded-r-md border border-l-0 inline-flex items-center transition-colors",
                                    active
                                        ? "border-sky-300 bg-sky-50 text-sky-700 hover:text-sky-900"
                                        : "border-slate-200 bg-white text-slate-400 hover:text-slate-700 hover:border-slate-300",
                                )}
                            >
                                <ExternalLinkIcon className="w-3 h-3" />
                            </Link>
                            {held && onReenrol && (
                                <button
                                    type="button"
                                    onClick={() => onReenrol(l)}
                                    disabled={reenrolling === l.segment_id}
                                    className="ml-1 h-6 px-1.5 rounded-md text-[11px] text-amber-800 hover:bg-amber-50 inline-flex items-center gap-1 transition-colors disabled:opacity-50"
                                >
                                    {reenrolling === l.segment_id ? (
                                        <Loader2Icon className="w-3 h-3 animate-spin" />
                                    ) : (
                                        <UserPlusIcon className="w-3 h-3" />
                                    )}
                                    Add back
                                </button>
                            )}
                        </li>
                    );
                })}
            </ul>
            <button
                type="button"
                onClick={onManage}
                className="ml-auto h-6 px-2 rounded-md text-[11.5px] text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
            >
                Manage
            </button>
        </div>
    );
}
