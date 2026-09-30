import React from "react";
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from "vitest";
import { screen, act, fireEvent, cleanup } from "@testing-library/react";
import { useAppStore } from "@/stores";
import { installLayoutShims, mount, setViewportWidth, settle, SUITE } from "./uniboxHarness";

beforeAll(() => {
    installLayoutShims();
    setViewportWidth(1512);
});

vi.mock("@/lib/api/client/Request", () => ({
    default: async (cfg: { url?: string }) => {
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

const separator = () => screen.getByRole("separator", { name: "Resize the scope rail" });
const rail = () => document.getElementById("unibox-scope-rail")!;
const railVar = () => rail().style.getPropertyValue("--unibox-rail-w");
const originalStorage = useAppStore.persist.getOptions().storage;

describe("resizable unibox scope rail", SUITE, () => {
    beforeEach(() => {
        useAppStore.setState({ navCollapsed: false, uniboxRailWidth: 220, uniboxListWidth: 360 });
    });

    afterEach(() => {
        cleanup();
        useAppStore.persist.setOptions({ storage: originalStorage });
        vi.mocked(localStorage.getItem).mockReset();
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it("renders an accessible 220px separator beside the rail, hidden below lg", async () => {
        expect(useAppStore.getInitialState().uniboxRailWidth).toBe(220);
        await mount("/app/unibox");
        await settle();
        expect(separator()).toHaveAttribute("aria-valuenow", "220");
        expect(separator()).toHaveAttribute("aria-valuemin", "180");
        expect(separator()).toHaveAttribute("aria-valuemax", "360");
        expect(separator()).toHaveAttribute("aria-orientation", "vertical");
        expect(separator()).toHaveAttribute("aria-controls", "unibox-scope-rail");
        expect(separator().previousElementSibling).toBe(rail());
        expect(separator().nextElementSibling).toHaveAttribute("id", "unibox-conversation-list");
        expect(separator().className).toContain("hidden lg:flex");
        expect(rail().className).toContain("hidden lg:flex");
        expect(railVar()).toBe("220px");
    });

    it("changes the pane and store with arrow keys and resets with Enter and double-click", async () => {
        await mount();
        await settle();
        act(() => fireEvent.keyDown(separator(), { key: "ArrowRight" }));
        expect(useAppStore.getState().uniboxRailWidth).toBe(236);
        expect(railVar()).toBe("236px");
        expect(separator()).toHaveAttribute("aria-valuenow", "236");
        act(() => fireEvent.keyDown(separator(), { key: "ArrowLeft", shiftKey: true }));
        expect(useAppStore.getState().uniboxRailWidth).toBe(188);
        expect(railVar()).toBe("188px");
        act(() => fireEvent.keyDown(separator(), { key: "Enter" }));
        expect(useAppStore.getState().uniboxRailWidth).toBe(220);
        expect(railVar()).toBe("220px");
        act(() => fireEvent.keyDown(separator(), { key: "End" }));
        expect(useAppStore.getState().uniboxRailWidth).toBe(360);
        act(() => fireEvent.doubleClick(separator()));
        expect(useAppStore.getState().uniboxRailWidth).toBe(220);
        expect(railVar()).toBe("220px");
    });

    it("drags the pane, commits on release and stops at both bounds", async () => {
        await mount();
        await settle();
        act(() => {
            fireEvent.pointerDown(separator(), { clientX: 220, button: 0, pointerId: 1 });
            fireEvent.pointerMove(separator(), { clientX: 300, pointerId: 1 });
        });
        expect(railVar()).toBe("300px");
        expect(useAppStore.getState().uniboxRailWidth).toBe(220);
        act(() => fireEvent.pointerUp(separator(), { pointerId: 1 }));
        expect(useAppStore.getState().uniboxRailWidth).toBe(300);
        act(() => {
            fireEvent.pointerDown(separator(), { clientX: 300, button: 0, pointerId: 2 });
            fireEvent.pointerMove(separator(), { clientX: 4000, pointerId: 2 });
            fireEvent.pointerUp(separator(), { pointerId: 2 });
        });
        expect(useAppStore.getState().uniboxRailWidth).toBe(360);
        expect(railVar()).toBe("360px");
        act(() => {
            fireEvent.pointerDown(separator(), { clientX: 360, button: 0, pointerId: 3 });
            fireEvent.pointerMove(separator(), { clientX: 0, pointerId: 3 });
            fireEvent.pointerUp(separator(), { pointerId: 3 });
        });
        expect(useAppStore.getState().uniboxRailWidth).toBe(180);
        expect(railVar()).toBe("180px");
    });

    it.each([
        [4, 180],
        [99999, 360],
        [null, 220],
        ["300", 220],
        [undefined, 220],
    ])("rehydrates stored width %s as %s", async (stored, expected) => {
        vi.mocked(localStorage.getItem).mockImplementation((key) => key === "warmbly-storage"
            ? JSON.stringify({ state: { uniboxRailWidth: stored } })
            : null);
        await useAppStore.persist.rehydrate();
        expect(useAppStore.getState().uniboxRailWidth).toBe(expected);
    });

    it("persists a chosen width and restores it on rehydration", async () => {
        await mount();
        await settle();
        vi.mocked(localStorage.setItem).mockClear();
        act(() => fireEvent.keyDown(separator(), { key: "ArrowRight" }));
        const saved = vi.mocked(localStorage.setItem).mock.calls
            .filter(([key]) => key === "warmbly-storage").at(-1)![1];
        expect(JSON.parse(saved).state.uniboxRailWidth).toBe(236);
        act(() => useAppStore.getState().setUniboxRailWidth(220));
        vi.mocked(localStorage.getItem).mockImplementation((key) => key === "warmbly-storage" ? saved : null);
        await act(async () => { await useAppStore.persist.rehydrate(); });
        expect(useAppStore.getState().uniboxRailWidth).toBe(236);
        expect(railVar()).toBe("236px");
    });

    it("re-measures the list cap after a rail change and during a drag", async () => {
        let railResize: (() => void) | undefined;
        vi.stubGlobal("ResizeObserver", class {
            constructor(private sync: () => void) {}
            observe(el: Element) {
                if (el.id === "unibox-scope-rail") railResize = this.sync;
            }
            unobserve() {}
            disconnect() {}
        });
        const originalRect = Element.prototype.getBoundingClientRect;
        vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
            const list = document.getElementById("unibox-conversation-list");
            if (this === list?.parentElement) return new DOMRect(0, 0, 1100, 800);
            if (this === list) {
                const left = parseFloat(railVar()) + 6;
                return new DOMRect(left, 0, 620, 800);
            }
            return originalRect.call(this);
        });
        useAppStore.setState({ uniboxListWidth: 620 });
        await mount();
        await settle();
        const listSeparator = () => screen.getByRole("separator", { name: "Resize the conversation list" });
        // 1100 - (220 + 6) - 6px list handle - 360px thread reserve.
        expect(listSeparator()).toHaveAttribute("aria-valuemax", "508");
        act(() => fireEvent.keyDown(separator(), { key: "End" }));
        expect(listSeparator()).toHaveAttribute("aria-valuemax", "368");
        expect(listSeparator()).toHaveAttribute("aria-valuenow", "368");
        act(() => fireEvent.keyDown(separator(), { key: "Enter" }));
        expect(listSeparator()).toHaveAttribute("aria-valuemax", "508");
        expect(railResize).toBeTypeOf("function");
        act(() => {
            fireEvent.pointerDown(separator(), { clientX: 220, button: 0, pointerId: 1 });
            fireEvent.pointerMove(separator(), { clientX: 300, pointerId: 1 });
            railResize!();
        });
        expect(listSeparator()).toHaveAttribute("aria-valuemax", "428");
        expect(useAppStore.getState().uniboxRailWidth).toBe(220);
        expect(useAppStore.getState().uniboxListWidth).toBe(620);
        act(() => fireEvent.pointerUp(separator(), { pointerId: 1 }));
    });
});
