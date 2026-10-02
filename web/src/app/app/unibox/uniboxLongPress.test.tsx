import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from "vitest";
import { screen, act, fireEvent, waitFor, within } from "@testing-library/react";
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

const rowFor = (i: number) =>
    screen.getAllByText(ROWS[i].subject)
        .map((el) => el.closest<HTMLElement>('[role="button"]'))
        .find((row) => row?.querySelector('[aria-label="Conversation actions"]'))!;

// jsdom has no PointerEvent constructor; keep coordinates and pointer metadata real.
function pointer(node: Element, type: string, overrides: Record<string, unknown> = {}) {
    const { pointerType = "touch", pointerId = 1, isPrimary = true, ...coords } = overrides;
    const event = new MouseEvent(type, { bubbles: true, cancelable: true, clientX: 120, clientY: 160, ...coords });
    Object.defineProperties(event, {
        pointerType: { value: pointerType },
        pointerId: { value: pointerId },
        isPrimary: { value: isPrimary },
    });
    fireEvent(node, event);
}

async function ready() {
    const router = await mount("/app/unibox/awaiting");
    await settle();
    await waitFor(() => expect(rowFor(0)).toBeTruthy());
    vi.useFakeTimers();
    return router;
}

function hold(node: Element) {
    act(() => {
        pointer(node, "pointerdown");
        vi.advanceTimersByTime(499);
    });
    expect(screen.queryByRole("menu")).toBeNull();
    act(() => { vi.advanceTimersByTime(1); });
    expect(screen.queryByRole("menu")).not.toBeNull();
}

describe("unibox touch context menus", SUITE, () => {
    beforeEach(() => {
        calls.length = 0;
        resetScrollTops();
        setViewportWidth(1512);
    });
    afterEach(() => { vi.useRealTimers(); });

    it("opens the conversation menu at the finger and swallows the release click", async () => {
        const router = await ready();
        const row = rowFor(0);
        hold(row);
        const menu = screen.getByRole("menu");
        expect(within(menu).getByRole("menuitem", { name: "Archive" })).toBeTruthy();
        expect(menu.style.left).toBe("120px");
        expect(menu.style.top).toBe("166px");
        act(() => {
            pointer(row, "pointerup");
            fireEvent.click(row);
        });
        expect(router.state.location.pathname).toBe("/app/unibox/awaiting");
        expect(screen.getAllByRole("menu")).toHaveLength(1);
    });

    it("swallows the release click on a portaled menu item", async () => {
        await ready();
        const row = rowFor(0);
        hold(row);
        const item = within(screen.getByRole("menu")).getByRole("menuitem", { name: "Open in new tab" });
        const open = vi.spyOn(window, "open").mockImplementation(() => null);
        try {
            act(() => {
                pointer(row, "pointerup");
                fireEvent.click(item);
            });
            expect(open).not.toHaveBeenCalled();
            expect(screen.getByRole("menu")).toBeTruthy();
        } finally {
            open.mockRestore();
        }
    });

    it("allows a new tap on a portaled menu item after a hold", async () => {
        await ready();
        const row = rowFor(0);
        hold(row);
        const item = within(screen.getByRole("menu")).getByRole("menuitem", { name: "Open in new tab" });
        const open = vi.spyOn(window, "open").mockImplementation(() => null);
        try {
            act(() => {
                pointer(row, "pointerup");
                pointer(item, "pointerdown");
                pointer(item, "pointerup");
                fireEvent.click(item);
            });
            expect(open).toHaveBeenCalledExactlyOnceWith(`/app/unibox/awaiting/${ROWS[0].thread_id}`, "_blank", "noopener");
        } finally {
            open.mockRestore();
        }
    });

    it("keeps a short touch tap opening the conversation", async () => {
        const router = await ready();
        act(() => {
            pointer(rowFor(0), "pointerdown");
            vi.advanceTimersByTime(100);
            pointer(rowFor(0), "pointerup");
            fireEvent.click(rowFor(0));
        });
        vi.useRealTimers();
        await waitFor(() => expect(router.state.location.pathname).toBe(`/app/unibox/awaiting/${ROWS[0].thread_id}`));
        expect(screen.queryByRole("menu")).toBeNull();
        vi.useFakeTimers();
        hold(rowFor(0));
        expect(router.state.location.pathname).toBe(`/app/unibox/awaiting/${ROWS[0].thread_id}`);
    });

    it("cancels a touch hold that moves more than ten pixels", async () => {
        await ready();
        act(() => {
            pointer(rowFor(0), "pointerdown");
            pointer(rowFor(0), "pointermove", { clientX: 131 });
            vi.advanceTimersByTime(600);
            pointer(rowFor(0), "pointerup");
        });
        expect(screen.queryByRole("menu")).toBeNull();
        hold(rowFor(0));
        expect(screen.getByRole("menu")).toBeTruthy();
    });

    it.each(["mouse", "pen"])("does not open for a %s hold and preserves right-click", async (pointerType) => {
        await ready();
        act(() => {
            pointer(rowFor(0), "pointerdown", { pointerType });
            vi.advanceTimersByTime(600);
            pointer(rowFor(0), "pointerup", { pointerType });
        });
        expect(screen.queryByRole("menu")).toBeNull();
        hold(rowFor(0));
        act(() => {
            pointer(rowFor(0), "pointerup");
            pointer(rowFor(0), "pointerdown", { pointerType });
            pointer(rowFor(0), "pointerup", { pointerType });
            fireEvent.contextMenu(rowFor(0), { clientX: 125, clientY: 165 });
        });
        expect(screen.getAllByRole("menu")).toHaveLength(1);
        expect(screen.getByRole("menu").style.left).toBe("125px");
    });

    it("ignores Android contextmenu during and just after a fired hold", async () => {
        const router = await ready();
        const row = rowFor(0);
        hold(row);
        const menu = screen.getByRole("menu");
        act(() => { fireEvent.contextMenu(row, { clientX: 125, clientY: 165 }); });
        expect(screen.getByRole("menu")).toBe(menu);
        expect(menu.style.left).toBe("120px");
        act(() => {
            pointer(row, "pointerup");
            vi.advanceTimersByTime(100);
            fireEvent.contextMenu(row, { clientX: 125, clientY: 165 });
            fireEvent.click(row);
        });
        expect(screen.getAllByRole("menu")).toHaveLength(1);
        expect(menu.style.left).toBe("120px");
        expect(router.state.location.pathname).toBe("/app/unibox/awaiting");
        const open = vi.spyOn(window, "open").mockImplementation(() => null);
        act(() => { fireEvent.click(within(menu).getByRole("menuitem", { name: "Open in new tab" })); });
        expect(open).toHaveBeenCalledWith(`/app/unibox/awaiting/${ROWS[0].thread_id}`, "_blank", "noopener");
        open.mockRestore();
    });

    it("opens the scope rail row's menu without navigating on release", async () => {
        const router = await ready();
        const row = document.querySelector('[data-rail-row="folder:inbox"]')!;
        expect(row).toBeTruthy();
        hold(row);
        expect(within(screen.getByRole("menu")).getByRole("menuitem", { name: "Mark all as read" })).toBeTruthy();
        act(() => {
            pointer(row, "pointerup");
            fireEvent.click(row);
        });
        expect(router.state.location.pathname).toBe("/app/unibox/awaiting");
    });

    it("opens the scope section menu without folding the section on release", async () => {
        await ready();
        const header = screen.getByRole("button", { name: "Mail" });
        hold(header);
        expect(screen.getByRole("menu")).toBeTruthy();
        act(() => {
            pointer(header, "pointerup");
            fireEvent.click(header);
        });
        expect(header.getAttribute("aria-expanded")).toBe("true");
    });

    it("uses the whole selection for a hold on a ticked conversation", async () => {
        await ready();
        act(() => {
            fireEvent.click(rowFor(0).querySelector('input[type="checkbox"]')!);
            fireEvent.click(rowFor(1).querySelector('input[type="checkbox"]')!);
        });
        hold(rowFor(1));
        expect(screen.getByText("2 conversations")).toBeTruthy();
        expect(screen.queryByRole("menuitem", { name: "Open in new tab" })).toBeNull();
    });
});
