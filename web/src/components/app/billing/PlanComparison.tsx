// Side-by-side plan comparison for Billing > Plans.
//
// Numbers come from the server plan records where the backend has them
// configured (that is what actually gets enforced), and fall back to the
// marketing catalog when a plan is not wired up. The workspace's current plan
// column is highlighted so "what would change" is readable at a glance.

import React from "react";
import { CheckIcon, MinusIcon } from "lucide-react";
import type ServerPlan from "@/lib/api/models/app/subscription/Plan";
import {
    PAID_PLANS,
    PLAN_ACCENT_CLASSES,
    getPlan,
    isAtLeast,
    type PlanID,
} from "@/lib/plans";
import { cn } from "@/lib/utils";
import type { BillingInterval } from "@/lib/pricing";

type Cell = React.ReactNode;

interface CompareRow {
    label: string;
    hint?: string;
    /** Starts a new labelled band in the table. */
    group?: string;
    cell: (id: PlanID, server: ServerPlan | undefined, interval: BillingInterval) => Cell;
}

const yes = <CheckIcon className="w-3.5 h-3.5 text-emerald-600 mx-auto" />;
const no = <MinusIcon className="w-3.5 h-3.5 text-slate-300 mx-auto" />;

function num(v: number | null | undefined, fallback: number): string {
    const n = v == null || v <= 0 ? fallback : v;
    if (!Number.isFinite(n)) return "Custom";
    return n.toLocaleString();
}

const ROWS: CompareRow[] = [
    {
        group: "Price",
        label: "Per month",
        hint: "Billed at the interval selected above",
        cell: (id, _s, interval) => {
            const p = getPlan(id);
            const v = interval === "annual" ? p.priceAnnual : p.priceMonthly;
            return v == null ? "Custom" : `$${v}`;
        },
    },
    {
        label: "Per year",
        cell: (id, _s, interval) => {
            const p = getPlan(id);
            const v = interval === "annual" ? p.priceAnnual : p.priceMonthly;
            return v == null ? "Custom" : `$${(v * 12).toLocaleString()}`;
        },
    },
    {
        group: "Limits",
        label: "Emails per day",
        hint: "Campaign sends across the workspace",
        cell: (id, s) => num(s?.limits?.max_emails_per_day, getPlan(id).sendsPerDay),
    },
    {
        label: "Mailboxes",
        hint: "Connected sending and warmup mailboxes",
        cell: (_id, s) => (s?.limits?.max_email_accounts ? num(s.limits.max_email_accounts, 0) : "Unlimited"),
    },
    {
        label: "Contacts",
        cell: (_id, s) => (s?.limits?.max_contacts ? num(s.limits.max_contacts, 0) : "Unlimited"),
    },
    {
        label: "Campaigns",
        cell: (_id, s) => (s?.limits?.max_campaigns ? num(s.limits.max_campaigns, 0) : "Unlimited"),
    },
    {
        label: "Team members",
        cell: (_id, s) => (s?.limits?.max_team_members ? num(s.limits.max_team_members, 0) : "Unlimited"),
    },
    {
        group: "Capabilities",
        label: "Mailbox warmup",
        hint: "Unlimited on every paid plan",
        cell: () => yes,
    },
    { label: "Campaign sending", cell: () => yes },
    { label: "Unified inbox", cell: () => yes },
    { label: "Contacts and CRM", cell: () => yes },
    { label: "Automations and integrations", cell: () => yes },
    { label: "Team members and roles", cell: (id) => (isAtLeast(id, "starter") ? yes : no) },
    { label: "Bulk contact operations", cell: (id) => (isAtLeast(id, "starter") ? yes : no) },
    { label: "Webhooks", cell: (id) => (isAtLeast(id, "business") ? yes : no) },
    {
        label: "Advanced outreach",
        hint: "A/B tests and custom sending rules",
        cell: (id) => (isAtLeast(id, "business") ? yes : no),
    },
    { label: "Dedicated IPs", cell: (id) => (getPlan(id).dedicatedIps ? yes : no) },
    { label: "Dedicated support", cell: (id) => (id === "enterprise" ? yes : no) },
];

export default function PlanComparison({
    current,
    interval,
    resolveServerPlan,
}: {
    current: PlanID;
    interval: BillingInterval;
    /** Maps a catalog plan to the server record so real limits win. */
    resolveServerPlan: (id: PlanID) => ServerPlan | undefined;
}) {
    return (
        <div className="overflow-x-auto -mx-1 px-1">
            <table className="w-full min-w-[620px] text-[12px] border-separate border-spacing-0">
                <thead>
                    <tr>
                        <th className="sticky left-0 z-10 bg-white text-left font-medium text-[10.5px] uppercase tracking-[0.08em] text-slate-400 px-3 py-2 border-b border-slate-200">
                            Feature
                        </th>
                        {PAID_PLANS.map((id) => {
                            const plan = getPlan(id);
                            const accent = PLAN_ACCENT_CLASSES[plan.accent];
                            const isCurrent = id === current;
                            return (
                                <th
                                    key={id}
                                    className={cn(
                                        "px-3 py-2 border-b border-slate-200 text-center align-bottom",
                                        isCurrent && "bg-slate-50",
                                    )}
                                >
                                    <span className="inline-flex items-center gap-1.5">
                                        <span className={cn("size-1.5 rounded-full", accent.dot)} />
                                        <span className="text-[11.5px] font-semibold text-slate-800">
                                            {plan.label}
                                        </span>
                                    </span>
                                    {isCurrent && (
                                        <span className="block text-[9px] uppercase tracking-[0.08em] font-semibold text-slate-500 mt-0.5">
                                            Current
                                        </span>
                                    )}
                                </th>
                            );
                        })}
                    </tr>
                </thead>
                <tbody>
                    {ROWS.map((row) => (
                        <React.Fragment key={row.label}>
                            {row.group && (
                                <tr>
                                    <td
                                        colSpan={PAID_PLANS.length + 1}
                                        className="sticky left-0 bg-white px-3 pt-4 pb-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium"
                                    >
                                        {row.group}
                                    </td>
                                </tr>
                            )}
                            <tr className="group">
                                <th
                                    scope="row"
                                    className="sticky left-0 z-10 bg-white group-hover:bg-slate-50/80 text-left font-normal px-3 py-1.5 border-b border-slate-100 transition-colors"
                                >
                                    <span className="text-slate-700">{row.label}</span>
                                    {row.hint && (
                                        <span className="block text-[10.5px] text-slate-400 leading-snug">
                                            {row.hint}
                                        </span>
                                    )}
                                </th>
                                {PAID_PLANS.map((id) => (
                                    <td
                                        key={id}
                                        className={cn(
                                            "px-3 py-1.5 border-b border-slate-100 text-center tabular-nums text-slate-700 group-hover:bg-slate-50/80 transition-colors",
                                            id === current && "bg-slate-50 font-medium text-slate-900",
                                        )}
                                    >
                                        {row.cell(id, resolveServerPlan(id), interval)}
                                    </td>
                                ))}
                            </tr>
                        </React.Fragment>
                    ))}
                </tbody>
            </table>
        </div>
    );
}
