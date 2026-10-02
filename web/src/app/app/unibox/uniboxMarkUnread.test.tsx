// Mark as unread has to stick: the open reader marks what it shows as read,
// so no path that marks the open conversation unread may be followed by a read.
// In a browser the URL follows the store inside a router transition, so the
// reader outlives the press; navigation is deferred here to reproduce that.

import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach } from "vitest";
import { screen, act, fireEvent, waitFor } from "@testing-library/react";
import type * as ReactRouterDom from "react-router-dom";
import {
    installLayoutShims,
    mount,
    resetScrollTops,
    ROWS,
    setViewportWidth,
    settle,
    SUITE,
} from "./uniboxHarness";

type Call = { method?: string; url?: string; data?: Record<string, unknown> };

const calls = vi.hoisted((): Call[] => []);

beforeAll(() => {
    installLayoutShims();
    setViewportWidth(1512);
});

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: Call) => {
        calls.push({ method: cfg.method, url: cfg.url, data: cfg.data });
        const { route } = await import("./uniboxHarness");
        return route(String(cfg?.url ?? ""));
    },
}));
vi.mock("react-router-dom", async (orig) => {
    const actual = (await orig()) as typeof ReactRouterDom;
    return {
        ...actual,
        useNavigate: () => {
            const navigate = actual.useNavigate();
            return ((...args: Parameters<typeof navigate>) => {
                setTimeout(() => void navigate(...args), 100);
            }) as typeof navigate;
        },
    };
});
vi.mock("@/lib/helper/getToken", () => ({
    default: () => ({
        access_token: "a",
        refresh_token: "r",
        access_token_expires_at: new Date(Date.now() + 3600e3).toISOString(),
        refresh_token_expires_at: new Date(Date.now() + 3600e3).toISOString(),
    }),
}));
vi.mock("@/hooks/SocketProvider", () => ({
    default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/hooks/context/socket", async (orig) => {
    const actual = (await orig()) as Record<string, unknown>;
    return {
        ...actual,
        useSocket: () => ({
            isConnected: false,
            subscribeToChannel: () => () => {},
            pushToChannel: () => {},
            socket: null,
            status: "closed",
        }),
        useChannel: () => ({ state: "closed", push: () => {}, channel: null }),
        useChannelEvent: () => {},
        useChannelSubscription: () => {},
    };
});

const seenPatches = () =>
    calls.filter((c) => c.method === "PATCH" && c.url === "/unibox/seen");

const bodyGets = () =>
    calls.filter((c) => c.method === "GET" && ROWS.some((row) => c.url === `/unibox/${row.id}`));

const rowFor = (i: number) =>
    screen
        .getAllByText(ROWS[i].subject)
        .map((el) => el.closest<HTMLElement>('[role="button"]'))
        .find((row) => row?.querySelector('[aria-label="Conversation actions"]'))!;

describe("unibox mark as unread", SUITE, () => {
    beforeEach(() => {
        calls.length = 0;
        resetScrollTops();
        setViewportWidth(1512);
    });

    it("keeps a conversation unread when the reader's button marks it", async () => {
        const router = await mount(`/app/unibox/awaiting/${ROWS[0].thread_id}`);
        await settle();

        calls.length = 0;
        await act(async () => {
            fireEvent.click(screen.getAllByRole("button", { name: "Mark as unread" })[0]);
        });
        await settle();
        await settle();

        await waitFor(() => expect(router.state.location.pathname).toBe("/app/unibox/awaiting"));
        const patches = seenPatches();
        expect(patches.length).toBeGreaterThan(0);
        expect(patches.every((p) => p.data?.seen === false)).toBe(true);
        expect(bodyGets()).toEqual([]);
        // By conversation only: naming every message would mark our own sent
        // copies unread too, where the server picks the newest received one.
        expect(patches[0].data).toMatchObject({ email_ids: [], thread_ids: [ROWS[0].thread_id] });
    });

    it("keeps the open conversation unread when its row menu marks it", async () => {
        await mount(`/app/unibox/awaiting/${ROWS[0].thread_id}`);
        await settle();

        await act(async () => {
            fireEvent.contextMenu(rowFor(0), { clientX: 120, clientY: 160 });
        });
        await settle();
        calls.length = 0;
        await act(async () => {
            fireEvent.click(screen.getByRole("menuitem", { name: "Mark as unread" }));
        });
        await settle();
        await settle();

        const patches = seenPatches();
        expect(patches.length).toBeGreaterThan(0);
        expect(patches.every((p) => p.data?.seen === false)).toBe(true);
        expect(bodyGets()).toEqual([]);
    });
});
