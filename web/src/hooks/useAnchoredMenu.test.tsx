import React from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAnchoredMenu } from "./useAnchoredMenu";

function Row({ onClick = () => {} }: { onClick?: () => void }) {
  const menu = useAnchoredMenu();
  return (
    <div data-testid="row" onClick={onClick} onContextMenu={menu.onContextMenu} {...menu.longPressProps}>
      <output>{menu.fromPointer ? `${menu.menuProps.anchorPoint?.x},${menu.menuProps.anchorPoint?.y}` : "closed"}</output>
    </div>
  );
}

function pointer(type: string, overrides: Record<string, unknown> = {}) {
  const { pointerId = 1, isPrimary = true, ...coords } = overrides;
  const event = new MouseEvent(type, { bubbles: true, clientX: 120, clientY: 160, ...coords });
  Object.defineProperties(event, {
    pointerType: { value: "touch" },
    pointerId: { value: pointerId },
    isPrimary: { value: isPrimary },
  });
  fireEvent(screen.getByTestId("row"), event);
}

function hold() {
  act(() => {
    pointer("pointerdown");
    vi.advanceTimersByTime(500);
  });
  expect(screen.getByText("120,160")).toBeTruthy();
}

describe("useAnchoredMenu touch lifecycle", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it.each(["pointerup", "pointercancel"])("cancels a pending hold on %s", (type) => {
    render(<Row />);
    act(() => {
      pointer("pointerdown");
      vi.advanceTimersByTime(499);
      pointer(type);
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText("closed")).toBeTruthy();
    hold();
  });

  it("clears its pending timer on unmount", () => {
    const row = render(<Row />);
    act(() => { pointer("pointerdown"); });
    expect(vi.getTimerCount()).toBe(1);
    row.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("ignores non-primary touch pointers", () => {
    render(<Row />);
    act(() => {
      pointer("pointerdown", { isPrimary: false });
      vi.advanceTimersByTime(600);
    });
    expect(screen.getByText("closed")).toBeTruthy();
    hold();
  });

  it("keeps the hold when a different pointer ends or moves", () => {
    render(<Row />);
    act(() => {
      pointer("pointerdown");
      pointer("pointermove", { pointerId: 2, clientX: 200 });
      pointer("pointerup", { pointerId: 2 });
      vi.advanceTimersByTime(500);
    });
    expect(screen.getByText("120,160")).toBeTruthy();
  });

  it("allows ten pixels of finger drift", () => {
    render(<Row />);
    act(() => {
      pointer("pointerdown");
      pointer("pointermove", { clientX: 126, clientY: 168 });
      vi.advanceTimersByTime(500);
    });
    expect(screen.getByText("120,160")).toBeTruthy();
  });

  it("suppresses the release even after a prolonged hold, but allows a new tap", () => {
    const click = vi.fn();
    render(<Row onClick={click} />);
    hold();
    act(() => {
      vi.advanceTimersByTime(2000);
      fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 125, clientY: 165 });
      pointer("pointerup");
      fireEvent.click(screen.getByTestId("row"));
    });
    expect(screen.getByText("120,160")).toBeTruthy();
    expect(click).not.toHaveBeenCalled();
    act(() => {
      pointer("pointerdown");
      pointer("pointerup");
      fireEvent.click(screen.getByTestId("row"));
    });
    expect(click).toHaveBeenCalledTimes(1);
  });

  it.each([false, true])("prevents touchend after a fired hold (pointerup first: %s)", (released) => {
    render(<Row />);
    hold();
    act(() => { vi.advanceTimersByTime(2000); });
    if (released) pointer("pointerup");
    const end = new Event("touchend", { bubbles: true, cancelable: true });
    fireEvent(screen.getByTestId("row"), end);
    expect(end.defaultPrevented).toBe(true);
  });

  it("does not prevent touchend after a short tap", () => {
    render(<Row />);
    pointer("pointerdown");
    const end = new Event("touchend", { bubbles: true, cancelable: true });
    fireEvent(screen.getByTestId("row"), end);
    expect(end.defaultPrevented).toBe(false);
    pointer("pointerup");
  });

  it("does not reopen from the timer when Android's native menu arrives first", () => {
    render(<Row />);
    act(() => {
      pointer("pointerdown");
      vi.advanceTimersByTime(400);
      fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 125, clientY: 165 });
      vi.advanceTimersByTime(200);
    });
    expect(screen.getByText("125,165")).toBeTruthy();
    expect(vi.getTimerCount()).toBe(0);
    hold();
  });

  it("expires native-menu suppression after release", () => {
    render(<Row />);
    hold();
    act(() => {
      pointer("pointerup");
      vi.advanceTimersByTime(801);
      fireEvent.contextMenu(screen.getByTestId("row"), { clientX: 125, clientY: 165 });
    });
    expect(screen.getByText("125,165")).toBeTruthy();
  });
});
