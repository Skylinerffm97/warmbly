// UpgradeDialog — the full-screen plan chooser every plan lock opens.
//
// A pricing page that lives inside the app: sky hero naming the locked
// feature, monthly/annual toggle, the four plans as large cards with the
// unlocking plan highlighted, and one-click checkout through useUpgradeFlow.
// Non-owners get the same comparison with an "ask the owner" path instead of
// a dead CTA. Mounted once by UpgradeDialogProvider; open it with
// useUpgradeDialog().open({ feature, minPlan }).

import React from "react";
import { createPortal } from "react-dom";
import { Link } from "react-router-dom";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import toast from "react-hot-toast";
import {
    ArrowRightIcon,
    ArrowUpRightIcon,
    CheckIcon,
    Loader2Icon,
    LockIcon,
    MinusIcon,
    ShieldCheckIcon,
    SparklesIcon,
    TicketIcon,
    XIcon,
} from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useUpgradeFlow from "@/hooks/useUpgradeFlow";
import type { UpgradeRequest } from "@/hooks/context/upgrade";
import useValidateDiscountCode from "@/lib/api/hooks/app/subscription/useValidateDiscountCode";
import type DiscountPreview from "@/lib/api/models/app/subscription/DiscountPreview";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { WEBSITE_URL } from "@/lib/information";
import {
    PAID_PLANS,
    PLAN_ACCENT_CLASSES,
    getPlan,
    isAtLeast,
    planOrder,
    type PlanID,
} from "@/lib/plans";
import {
    describeDiscount,
    discountedPrice,
    fmtMoney,
    type BillingInterval,
} from "@/lib/pricing";
import { TextInput } from "@/components/ui/field";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import BillingIntervalToggle from "@/components/app/billing/BillingIntervalToggle";
import { cn } from "@/lib/utils";

const EASE = [0.22, 1, 0.36, 1] as const;

export default function UpgradeDialog({
    open,
    request,
    onClose,
}: {
    open: boolean;
    request: UpgradeRequest;
    onClose: () => void;
}) {
    const access = useFeatureAccess();
    const flow = useUpgradeFlow();
    const validateCode = useValidateDiscountCode();
    const reduced = useReducedMotion();

    const [billingInterval, setBillingInterval] = React.useState<BillingInterval>("annual");
    const [promoOpen, setPromoOpen] = React.useState(false);
    const [codeInput, setCodeInput] = React.useState("");
    const [applied, setApplied] = React.useState<DiscountPreview | null>(null);

    const minPlan = getPlan(request.minPlan ?? "starter");
    const current = getPlan(access.plan).id;
    const busy = flow.pending !== null || flow.portalPending;

    // The card to spotlight: the cheapest plan that unlocks the feature and is
    // not below what the workspace already pays for.
    const recommended =
        PAID_PLANS.find((id) => isAtLeast(id, minPlan.id) && planOrder(id) > planOrder(current)) ??
        PAID_PLANS.find((id) => isAtLeast(id, minPlan.id)) ??
        "starter";

    // The card is marked data-floating so a dialog underneath ignores Escape
    // while this one is up. Escape here closes only this layer: any other
    // floating panel or the confirm owns it.
    const cardRef = React.useRef<HTMLDivElement>(null);
    React.useEffect(() => {
        if (!open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape") return;
            const others = Array.from(document.querySelectorAll("[data-floating]")).some(
                (el) => el !== cardRef.current,
            );
            if (others || document.querySelector("[role='alertdialog']")) return;
            if (busy) return;
            onClose();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [open, busy, onClose]);

    // Lock the page behind the dialog while it is open.
    React.useEffect(() => {
        if (!open) return;
        const prev = document.body.style.overflow;
        document.body.style.overflow = "hidden";
        return () => {
            document.body.style.overflow = prev;
        };
    }, [open]);

    async function applyCode() {
        const code = codeInput.trim();
        if (!code) return;
        try {
            const res = await validateCode.mutateAsync({ code });
            if (res.valid) {
                setApplied(res);
                toast.success("Promo code applied");
            } else {
                setApplied(null);
                toast.error(res.reason || "That code can't be applied");
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    async function choose(id: PlanID) {
        const outcome = await flow.upgrade(id, {
            interval: billingInterval,
            discountCode: applied?.valid ? applied.code : undefined,
            returnTo: window.location.pathname + window.location.search,
        });
        if (outcome === "changed") onClose();
    }

    const requestClose = () => {
        if (!busy) onClose();
    };

    const titleId = "upgrade-dialog-title";

    return createPortal(
        <AnimatePresence>
            {open && (
                <motion.div
                    key="upgrade-backdrop"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    onMouseDown={(e) => {
                        if (e.target === e.currentTarget) requestClose();
                    }}
                    className="fixed inset-0 z-[150] flex items-center justify-center bg-slate-900/45 backdrop-blur-[4px] p-3 sm:p-6"
                >
                    <motion.div
                        key="upgrade-card"
                        role="dialog"
                        aria-modal="true"
                        aria-labelledby={titleId}
                        data-floating
                        ref={cardRef}
                        initial={reduced ? { opacity: 0 } : { opacity: 0, y: 24, scale: 0.97 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={reduced ? { opacity: 0 } : { opacity: 0, y: 16, scale: 0.98 }}
                        transition={{ duration: 0.28, ease: EASE }}
                        onMouseDown={(e) => e.stopPropagation()}
                        className="relative w-full max-w-[1120px] max-h-[calc(100dvh-1.5rem)] sm:max-h-[calc(100dvh-3rem)] overflow-y-auto rounded-2xl bg-white border border-slate-200 shadow-[0_1px_2px_rgba(15,23,42,0.06),0_40px_90px_-30px_rgba(15,23,42,0.5)]"
                    >
                        <Hero
                            request={request}
                            planLabel={minPlan.label}
                            titleId={titleId}
                            onClose={requestClose}
                            busy={busy}
                        />

                        {/* Interval row */}
                        <div className="px-5 md:px-10 pt-6 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                            <div>
                                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                    Choose a plan
                                </div>
                                <p className="text-[12.5px] text-slate-500 mt-0.5">
                                    {billingInterval === "annual"
                                        ? "Annual billing, two months free every year."
                                        : "Monthly billing, switch or cancel any time."}
                                </p>
                            </div>
                            <BillingIntervalToggle interval={billingInterval} onChange={setBillingInterval} size="md" />
                        </div>

                        {/* Plan cards */}
                        <div className="px-5 md:px-10 pt-4 pb-6 grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3 items-stretch">
                            {PAID_PLANS.map((id, i) => (
                                <PlanCard
                                    key={id}
                                    id={id}
                                    index={i}
                                    feature={request.feature}
                                    minPlan={minPlan.id}
                                    current={current}
                                    recommended={id === recommended}
                                    interval={billingInterval}
                                    discount={applied}
                                    isOwner={access.isOwner}
                                    pending={flow.pending === id}
                                    busy={busy}
                                    onChoose={() => choose(id)}
                                />
                            ))}
                        </div>

                        {/* Promo code */}
                        <div className="px-5 md:px-10 pb-5">
                            {!promoOpen && !applied ? (
                                <button
                                    type="button"
                                    onClick={() => setPromoOpen(true)}
                                    className="inline-flex items-center gap-1.5 text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
                                >
                                    <TicketIcon className="w-3 h-3" />
                                    Have a promo code?
                                </button>
                            ) : (
                                <motion.div
                                    initial={reduced ? false : { opacity: 0, y: -4 }}
                                    animate={{ opacity: 1, y: 0 }}
                                    transition={{ duration: 0.18, ease: EASE }}
                                    className="flex flex-wrap items-center gap-2"
                                >
                                    <TextInput
                                        value={codeInput}
                                        onChange={(v) => setCodeInput(v.toUpperCase())}
                                        placeholder="WELCOME10"
                                        disabled={!!applied}
                                        autoFocus={!applied}
                                        onKeyDown={(e) => {
                                            if (e.key === "Enter") applyCode();
                                        }}
                                        className="w-[180px] font-mono uppercase"
                                    />
                                    {applied ? (
                                        <>
                                            <span className="text-[11.5px] text-emerald-700 bg-emerald-50 border border-emerald-100 rounded-md px-2 h-7 inline-flex items-center gap-1.5">
                                                <CheckIcon className="w-3 h-3 shrink-0" />
                                                <span className="font-mono font-medium">{applied.code}</span>
                                                <span>· {describeDiscount(applied)}</span>
                                            </span>
                                            <button
                                                type="button"
                                                onClick={() => {
                                                    setApplied(null);
                                                    setCodeInput("");
                                                }}
                                                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1"
                                            >
                                                <XIcon className="w-3 h-3" />
                                                Clear
                                            </button>
                                        </>
                                    ) : (
                                        <button
                                            type="button"
                                            onClick={applyCode}
                                            disabled={validateCode.isPending || !codeInput.trim()}
                                            className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                                        >
                                            {validateCode.isPending ? (
                                                <Loader2Icon className="w-3 h-3 animate-spin" />
                                            ) : (
                                                <TicketIcon className="w-3 h-3" />
                                            )}
                                            Apply
                                        </button>
                                    )}
                                </motion.div>
                            )}
                        </div>

                        {/* Footer */}
                        <div className="px-5 md:px-10 py-4 border-t border-slate-200 bg-slate-50/60 flex flex-col md:flex-row md:items-center gap-3 text-[12px] text-slate-500">
                            {access.isOwner ? (
                                <ul className="flex flex-wrap items-center gap-x-4 gap-y-1">
                                    <li className="inline-flex items-center gap-1.5">
                                        <ShieldCheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                                        Secure checkout by Stripe
                                    </li>
                                    <li className="inline-flex items-center gap-1.5">
                                        <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                                        Cancel anytime
                                    </li>
                                    <li className="inline-flex items-center gap-1.5">
                                        <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                                        Plan changes are prorated
                                    </li>
                                </ul>
                            ) : (
                                <p className="leading-relaxed">
                                    Only the workspace owner can change the plan. Ask them to upgrade to{" "}
                                    <span className="font-medium text-slate-900">{minPlan.label}</span>, or see who does
                                    what in{" "}
                                    <Link
                                        to="/app/settings/roles"
                                        onClick={onClose}
                                        className="font-medium text-slate-700 hover:text-slate-900 underline underline-offset-2"
                                    >
                                        Roles and access
                                    </Link>
                                    .
                                </p>
                            )}
                            <div className="md:ml-auto flex flex-wrap items-center gap-x-4 gap-y-1">
                                {access.isOwner && (
                                    <Link
                                        to="/app/settings/billing/plans"
                                        onClick={onClose}
                                        className="inline-flex items-center gap-1 font-medium text-slate-600 hover:text-slate-900 transition-colors"
                                    >
                                        Full billing page
                                        <ArrowRightIcon className="w-3 h-3" />
                                    </Link>
                                )}
                                <a
                                    href={`${WEBSITE_URL}/pricing`}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="inline-flex items-center gap-1 font-medium text-slate-600 hover:text-slate-900 transition-colors"
                                >
                                    Compare every feature
                                    <ArrowUpRightIcon className="w-3 h-3" />
                                </a>
                                <button
                                    type="button"
                                    onClick={requestClose}
                                    disabled={busy}
                                    className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 bg-white text-[12px] font-medium text-slate-700 hover:text-slate-900 transition-colors disabled:opacity-50"
                                >
                                    Not now
                                </button>
                            </div>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>,
        document.body,
    );
}

function Hero({
    request,
    planLabel,
    titleId,
    onClose,
    busy,
}: {
    request: UpgradeRequest;
    planLabel: string;
    titleId: string;
    onClose: () => void;
    busy: boolean;
}) {
    const reduced = useReducedMotion();
    const bullets = request.bullets?.slice(0, 4);
    const rise = (delay: number) =>
        reduced
            ? {}
            : {
                  initial: { opacity: 0, y: 10 },
                  animate: { opacity: 1, y: 0 },
                  transition: { duration: 0.4, ease: EASE, delay },
              };
    return (
        <div className="relative overflow-hidden px-5 pt-9 pb-8 md:px-10 md:pt-12 md:pb-10 text-center text-white">
            <div className="absolute inset-0" aria-hidden="true">
                <div className="sky-base" />
                <div className="sky-breathe" />
                <div className="sun-glow" />
                <img
                    src="/backdrops/cloud-3.webp"
                    alt=""
                    decoding="async"
                    className="cloud-drift cloud-1 absolute select-none"
                    style={{ top: "-4%", left: "-6%", width: 340, opacity: 0.5, height: "auto" }}
                />
                <img
                    src="/backdrops/cloud-4.webp"
                    alt=""
                    decoding="async"
                    className="cloud-drift cloud-2 absolute select-none"
                    style={{ bottom: "-10%", right: "-4%", width: 300, opacity: 0.45, height: "auto" }}
                />
                <img
                    src="/backdrops/cloud-1.webp"
                    alt=""
                    decoding="async"
                    className="cloud-drift cloud-1 absolute select-none"
                    style={{ top: "30%", right: "18%", width: 200, opacity: 0.3, height: "auto" }}
                />
                <div className="absolute inset-x-0 bottom-0 h-16 bg-gradient-to-t from-slate-900/25 to-transparent" />
            </div>

            <button
                type="button"
                onClick={onClose}
                disabled={busy}
                aria-label="Close"
                className="absolute top-3 right-3 z-10 size-8 rounded-md bg-white/15 hover:bg-white/25 text-white inline-flex items-center justify-center transition-colors disabled:opacity-50"
            >
                <XIcon className="w-4 h-4" />
            </button>

            <div className="relative z-10 max-w-2xl mx-auto">
                <motion.span
                    {...rise(0.05)}
                    className="inline-flex items-center gap-1.5 h-6 px-2.5 rounded-full bg-white/15 border border-white/25 text-[11px] font-medium backdrop-blur-sm"
                >
                    <LockIcon className="w-3 h-3" />
                    {request.feature} · {planLabel} and up
                </motion.span>
                <motion.h2
                    {...rise(0.12)}
                    id={titleId}
                    className="mt-4 text-[26px] md:text-[36px] font-semibold tracking-[-0.03em] leading-[1.08]"
                >
                    {request.feature} unlocks with {planLabel}.
                </motion.h2>
                <motion.p
                    {...rise(0.2)}
                    className="mt-3 text-[14px] md:text-[15px] text-sky-50/90 leading-relaxed max-w-xl mx-auto"
                >
                    {request.blurb ??
                        `Pick a plan and ${request.feature.toLowerCase()} turns on for your whole workspace the moment checkout completes.`}
                </motion.p>
                {bullets && bullets.length > 0 && (
                    <motion.ul
                        {...rise(0.28)}
                        className="mt-5 flex flex-wrap justify-center gap-x-4 gap-y-1.5 text-[12.5px] text-white/90"
                    >
                        {bullets.map((b) => (
                            <li key={b} className="inline-flex items-center gap-1.5">
                                <CheckIcon className="w-3 h-3 text-emerald-200 shrink-0" />
                                {b}
                            </li>
                        ))}
                    </motion.ul>
                )}
            </div>
        </div>
    );
}

function PlanCard({
    id,
    index,
    feature,
    minPlan,
    current,
    recommended,
    interval,
    discount,
    isOwner,
    pending,
    busy,
    onChoose,
}: {
    id: PlanID;
    index: number;
    feature: string;
    minPlan: PlanID;
    current: PlanID;
    recommended: boolean;
    interval: BillingInterval;
    discount: DiscountPreview | null;
    isOwner: boolean;
    pending: boolean;
    busy: boolean;
    onChoose: () => void;
}) {
    const reduced = useReducedMotion();
    const plan = getPlan(id);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];
    const unlocks = isAtLeast(id, minPlan);
    const isCurrent = id === current;
    const below = planOrder(id) < planOrder(current);
    const annual = interval === "annual";
    const base = annual ? plan.priceAnnual : plan.priceMonthly;
    const disc = discountedPrice(base, discount);
    const shown = disc ?? base;
    const custom = base == null;

    const sends =
        plan.sendsPerDay === Number.POSITIVE_INFINITY
            ? "Custom volume"
            : `${plan.sendsPerDay.toLocaleString()} emails / day`;

    const showCta = isOwner && !below;

    return (
        <motion.div
            initial={reduced ? false : { opacity: 0, y: 18 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.38, ease: EASE, delay: 0.12 + index * 0.07 }}
            whileHover={reduced || !unlocks ? undefined : { y: -3 }}
            className={cn(
                "relative rounded-xl border bg-white p-5 flex flex-col transition-shadow",
                recommended
                    ? cn("border-transparent ring-2 bg-gradient-to-b to-white", accent.ring, accent.soft)
                    : "border-slate-200 hover:shadow-[0_16px_40px_-24px_rgba(15,23,42,0.35)]",
                !unlocks && "opacity-70",
            )}
        >
            {recommended && (
                <motion.span
                    initial={reduced ? false : { opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3, ease: EASE, delay: 0.4 + index * 0.07 }}
                    className={cn(
                        "absolute -top-2.5 left-4 inline-flex items-center gap-1 h-5 px-2 rounded-full text-[10px] font-semibold uppercase tracking-[0.08em] text-white",
                        accent.button,
                    )}
                >
                    <SparklesIcon className="w-2.5 h-2.5" />
                    Unlocks {feature}
                </motion.span>
            )}

            <div className="flex items-center gap-2">
                <span className={cn("size-2 rounded-full", accent.dot)} />
                <span className="text-[12px] uppercase tracking-[0.1em] font-semibold text-slate-800">
                    {plan.label}
                </span>
                {isCurrent ? (
                    <span className="ml-auto text-[9px] uppercase tracking-[0.08em] font-semibold text-slate-700 bg-slate-100 border border-slate-200 rounded px-1">
                        Current
                    </span>
                ) : plan.featured ? (
                    <span className="ml-auto text-[9px] uppercase tracking-[0.08em] font-semibold text-indigo-700 bg-indigo-50 border border-indigo-100 rounded px-1">
                        Popular
                    </span>
                ) : null}
            </div>
            <p className="mt-1.5 text-[12px] text-slate-500 leading-snug min-h-[32px]">{plan.description}</p>

            {/* Price */}
            <div className="mt-4 flex items-baseline gap-1.5">
                {custom ? (
                    <span className="text-[30px] font-semibold tracking-[-0.03em] text-slate-900">Custom</span>
                ) : (
                    <>
                        <span
                            className={cn(
                                "text-[32px] font-semibold tracking-[-0.03em] tabular-nums leading-none",
                                disc != null ? "text-emerald-700" : "text-slate-900",
                            )}
                        >
                            $
                            <AnimatedNumber
                                value={shown as number}
                                duration={0.45}
                                format={(n) => fmtMoney(Math.round(n * 100) / 100)}
                            />
                        </span>
                        <span className="text-[12px] text-slate-500">/ mo</span>
                        {disc != null && (
                            <span className="text-[12px] text-slate-400 line-through tabular-nums">
                                ${fmtMoney(base as number)}
                            </span>
                        )}
                    </>
                )}
            </div>
            <div className="mt-1 text-[11px] text-slate-400 h-4 tabular-nums">
                {custom
                    ? "tailored to your volume"
                    : annual
                      ? `billed annually, $${fmtMoney((shown as number) * 12)} / yr`
                      : "billed monthly"}
            </div>

            {/* Headline limit */}
            <div className="mt-4 rounded-md bg-slate-50 border border-slate-200/70 px-2.5 py-2 text-[12px] font-medium text-slate-800">
                {sends}
            </div>

            <ul className="mt-3 space-y-1.5 flex-1">
                {plan.bullets.map((b) => (
                    <li key={b} className="flex items-start gap-1.5 text-[12px] text-slate-700 leading-snug">
                        <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                        <span>{b}</span>
                    </li>
                ))}
                <li
                    className={cn(
                        "flex items-start gap-1.5 text-[12px] leading-snug pt-1.5 mt-1.5 border-t border-slate-200/70",
                        unlocks ? "text-slate-900 font-medium" : "text-slate-400",
                    )}
                >
                    {unlocks ? (
                        <CheckIcon className="w-3 h-3 text-emerald-600 mt-0.5 shrink-0" />
                    ) : (
                        <MinusIcon className="w-3 h-3 text-slate-300 mt-0.5 shrink-0" />
                    )}
                    <span>{unlocks ? `Includes ${feature}` : `Does not include ${feature}`}</span>
                </li>
            </ul>

            {/* CTA */}
            <div className="mt-4">
                {isCurrent ? (
                    <div className="h-9 rounded-md bg-slate-100 text-slate-400 text-[12.5px] font-medium inline-flex items-center justify-center w-full cursor-default">
                        Current plan
                    </div>
                ) : below ? (
                    <div className="h-9 text-[11.5px] text-slate-400 inline-flex items-center justify-center w-full">
                        Below your current plan
                    </div>
                ) : showCta ? (
                    <button
                        type="button"
                        onClick={onChoose}
                        disabled={busy}
                        className={cn(
                            "h-9 w-full rounded-md text-[12.5px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60",
                            id === "enterprise"
                                ? "border border-slate-200 hover:border-slate-300 bg-white text-slate-800"
                                : recommended
                                  ? accent.button
                                  : "bg-slate-900 hover:bg-slate-800 text-white",
                        )}
                    >
                        {pending ? (
                            <>
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                                {id === "enterprise" ? "Opening…" : "Redirecting…"}
                            </>
                        ) : id === "enterprise" ? (
                            <>Talk to sales</>
                        ) : (
                            <>
                                Upgrade to {plan.label}
                                <ArrowRightIcon className="w-3.5 h-3.5" />
                            </>
                        )}
                    </button>
                ) : (
                    <div className="h-9 text-[11.5px] text-slate-400 inline-flex items-center justify-center w-full">
                        Owner can upgrade
                    </div>
                )}
            </div>
        </motion.div>
    );
}
