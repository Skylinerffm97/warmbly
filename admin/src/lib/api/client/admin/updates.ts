// /admin/instance/update : what is running, what is newest, and the button
// that applies it through the host-side updater.

import { Request } from "@/lib/api/client";

export interface RunningBuild {
    version: string;
    commit?: string;
    built_at?: string;
}

export interface LatestRelease {
    tag: string;
    name?: string;
    html_url?: string;
    published_at?: string;
    channel: string;
}

export interface UpdaterCheckout {
    branch: string;
    detached: boolean;
    commit: string;
    describe: string;
    remote_commit: string;
    behind: number;
    dirty: boolean;
    fetched_at: string;
    fetch_error?: string;
}

// An image install has no checkout: what it runs is the tag pinned in its
// .env, which the updater reports instead.
export interface UpdaterRelease {
    tag: string;
    prefix: string;
    // False for a moving channel tag (prod, dev), where re-pulling the same
    // name is itself an update.
    pinned: boolean;
}

export type UpdateJobStatus = "running" | "succeeded" | "failed";

export interface UpdateJob {
    id: string;
    status: UpdateJobStatus;
    target: string;
    step: string;
    started_at: string;
    finished_at?: string;
    error?: string;
    from_commit: string;
    to_commit?: string;
    // Absent on the top-bar poll; present when fetched with log=1.
    log?: string[] | null;
}

export type UpdaterStatus = "off" | "ok" | "unreachable";

export interface UpdaterView {
    configured: boolean;
    status: UpdaterStatus;
    error?: string;
    mode?: "compose" | "image" | "command";
    repo_dir?: string;
    // Exactly one of these: a checkout in compose and command mode, a release
    // in image mode.
    checkout?: UpdaterCheckout;
    release?: UpdaterRelease;
    job?: UpdateJob;
    last_job?: UpdateJob;
}

export interface UpdateState {
    running: RunningBuild;
    latest?: LatestRelease;
    update_available: boolean;
    reason?: "release" | "commits";
    checked_at?: string;
    check_error?: string;
    enabled: boolean;
    interval: string;
    channel: string;
    repo: string;
    updater: UpdaterView;
}

export function getUpdateState(withLog = false): Promise<UpdateState> {
    return Request({
        method: "GET",
        url: withLog ? "/admin/instance/update?log=1" : "/admin/instance/update",
        authorization: true,
    });
}

export function checkForUpdates(): Promise<UpdateState> {
    return Request({
        method: "POST",
        url: "/admin/instance/update/check",
        authorization: true,
    });
}

export function applyUpdate(target = "latest"): Promise<UpdateJob> {
    return Request({
        method: "POST",
        url: "/admin/instance/update/apply",
        data: { target },
        authorization: true,
    });
}
