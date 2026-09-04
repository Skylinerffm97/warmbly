import { useConnectionStatus } from "@/stores";

// How often a realtime-backed list refetches while the socket is not
// delivering. Slow on purpose: this is a safety net, not the primary path.
export const REALTIME_FALLBACK_INTERVAL_MS = 60_000;

// A react-query refetchInterval for a list kept current by realtime
// invalidation. False while the socket is connected, so a healthy dashboard
// polls nothing; a slow poll once it is not, so the list cannot sit on its
// staleTime with no event coming to refresh it. Deliberately keyed on the
// connection being down rather than on reported quality: a connected socket
// still delivers, and the indicator's quality signal covers latency, not loss.
export default function useRealtimeFallbackInterval(enabled = true): number | false {
    const { status } = useConnectionStatus();
    return enabled && status !== "connected" ? REALTIME_FALLBACK_INTERVAL_MS : false;
}
