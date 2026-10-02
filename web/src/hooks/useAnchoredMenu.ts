// Spread `menuProps` onto the PopoverMenu and `longPressProps` alongside `onContextMenu` on its row.

import React from "react";

export type AnchorPoint = { x: number; y: number };

export function useAnchoredMenu() {
  const [open, setOpen] = React.useState(false);
  const [point, setPoint] = React.useState<AnchorPoint | null>(null);
  const timer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const press = React.useRef<{ id: number; point: AnchorPoint; fired: boolean } | null>(null);
  const suppress = React.useRef({ click: false, until: 0 });
  const cancelTimer = React.useCallback(() => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  }, []);
  React.useEffect(() => cancelTimer, [cancelTimer]);
  const openAt = (p: AnchorPoint) => {
    setPoint(p);
    setOpen(true);
  };
  const endPress = (e: React.PointerEvent<HTMLElement>) => {
    if (e.pointerId !== press.current?.id) return;
    cancelTimer();
    if (press.current.fired) suppress.current.until = Date.now() + 800;
    press.current = null;
  };
  const longPressProps = {
    onPointerDown: (e: React.PointerEvent<HTMLElement>) => {
      // React also bubbles events from the portaled menu through its row.
      if (e.pointerType === "touch" && !e.isPrimary) return;
      suppress.current = { click: false, until: 0 };
      if (!e.currentTarget.contains(e.target as Node)) return;
      cancelTimer();
      press.current = null;
      if (e.pointerType !== "touch") return;
      const p = { id: e.pointerId, point: { x: e.clientX, y: e.clientY }, fired: false };
      press.current = p;
      timer.current = setTimeout(() => {
        timer.current = null;
        p.fired = true;
        suppress.current = { click: true, until: Date.now() + 800 };
        openAt(p.point);
      }, 500);
    },
    onPointerMove: (e: React.PointerEvent<HTMLElement>) => {
      const p = press.current;
      if (p && p.id === e.pointerId && Math.hypot(e.clientX - p.point.x, e.clientY - p.point.y) > 10) {
        cancelTimer();
      }
    },
    onPointerUp: endPress,
    onPointerCancel: endPress,
    onTouchEnd: (e: React.TouchEvent<HTMLElement>) => {
      // pointerup can clear the press before touchend arrives.
      if (press.current?.fired || (suppress.current.click && Date.now() < suppress.current.until)) {
        e.preventDefault();
      }
    },
    onClickCapture: (e: React.MouseEvent<HTMLElement>) => {
      if (suppress.current.click && (press.current?.fired || Date.now() < suppress.current.until)) {
        e.preventDefault();
        e.stopPropagation();
        suppress.current.click = false;
      }
    },
  };
  const onContextMenu = (e: React.MouseEvent<HTMLElement>) => {
    e.preventDefault();
    e.stopPropagation();
    if (!e.currentTarget.contains(e.target as Node)) return;
    // Android may send its native contextmenu during the hold or after release.
    if (press.current?.fired || Date.now() < suppress.current.until) return;
    cancelTimer();
    if (press.current) {
      press.current.fired = true;
      suppress.current = { click: true, until: Date.now() + 800 };
    }
    // A keyboard context-menu key reports 0,0: open under the element instead.
    const r = e.currentTarget.getBoundingClientRect();
    openAt(e.clientX || e.clientY ? { x: e.clientX, y: e.clientY } : { x: r.left + 12, y: r.bottom });
  };
  const menuProps = {
    open,
    anchorPoint: point,
    onOpenChange: (o: boolean) => {
      if (o) setPoint(null);
      setOpen(o);
    },
  };
  // `fromPointer` tells a right-click from the trigger, for menus that act on
  // more than their own row when right-clicked.
  return { open, fromPointer: open && point !== null, onContextMenu, longPressProps, menuProps };
}
