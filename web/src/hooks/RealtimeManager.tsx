import React, { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useSocket } from './context/socket'
import { useAppStore } from '@/stores'
import { useUserProfile } from './context/user'
import { useRealtimeEvents } from './useRealtimeEvents'
import { PresenceProvider } from './PresenceProvider'
import useUnseenCount from '@/lib/api/hooks/app/unibox/useUnseenCount'

// Heartbeat roundtrip bands behind the header's connection indicator. A healthy
// socket answers in tens of milliseconds, and the provider's own watchdog gives
// up at 8s and reconnects, so "poor" stays well under that.
const DEGRADED_LATENCY_MS = 500
const POOR_LATENCY_MS = 2000

export function RealtimeManager({ children }: { children: React.ReactNode }) {
  const { isConnected, reconnectAttempt, joinChannel, leaveChannel } = useSocket()
  const { user } = useUserProfile()
  const currentOrg = useAppStore((s) => s.currentOrganization)
  const setConnectionStatus = useAppStore((s) => s.setConnectionStatus)
  const setReconnectAttempt = useAppStore((s) => s.setReconnectAttempt)
  const setConnectionQuality = useAppStore((s) => s.setConnectionQuality)
  const addJoinedChannel = useAppStore((s) => s.addJoinedChannel)
  const removeJoinedChannel = useAppStore((s) => s.removeJoinedChannel)
  const setUnseenCount = useAppStore((s) => s.setUnseenCount)
  const wsLatencyMs = useAppStore((s) => s.wsLatencyMs)

  const queryClient = useQueryClient()
  const hadConnectionRef = useRef(false)

  // Catch-up on reconnect: events are fire-and-forget, so anything emitted
  // while the socket was down (laptop sleep, network blip) is gone forever.
  // Rather than trusting caches that may have silently diverged, mark every
  // query stale on RE-connect (not the initial connect) — active views
  // refetch immediately, background ones on next focus.
  //
  // Gated hard: only after a real outage (>5s down, long enough to have
  // actually missed events) and at most once every 30s. A flapping socket
  // otherwise refetch-storms every active query and the whole dashboard
  // visibly reloads over and over.
  const disconnectedAtRef = useRef<number | null>(null)
  const lastCatchupRef = useRef(0)
  useEffect(() => {
    if (!isConnected) {
      if (hadConnectionRef.current && disconnectedAtRef.current === null) {
        disconnectedAtRef.current = Date.now()
      }
      return
    }
    if (hadConnectionRef.current && disconnectedAtRef.current !== null) {
      const gap = Date.now() - disconnectedAtRef.current
      const sinceLast = Date.now() - lastCatchupRef.current
      if (gap > 5_000 && sinceLast > 30_000) {
        lastCatchupRef.current = Date.now()
        void queryClient.invalidateQueries()
      }
    }
    hadConnectionRef.current = true
    disconnectedAtRef.current = null
  }, [isConnected, queryClient])

  // Sync connection status to store
  useEffect(() => {
    if (isConnected) {
      setConnectionStatus('connected')
      setConnectionQuality('good')
      setReconnectAttempt(0)
    } else if (reconnectAttempt > 0) {
      setConnectionStatus('connecting')
      setConnectionQuality('poor')
      setReconnectAttempt(reconnectAttempt)
    } else {
      setConnectionStatus('disconnected')
      setConnectionQuality('disconnected')
    }
  }, [isConnected, reconnectAttempt, setConnectionStatus, setConnectionQuality, setReconnectAttempt])

  // Auto-join user channel. Topic uses the user UUID, not the email —
  // the realtime channel handler is `def join("user:" <> user_id, ...)`
  // and compares against `socket.assigns.user_id` which is the JWT
  // `sub` (UUID). Using email here would refuse the join.
  useEffect(() => {
    if (isConnected && user?.id) {
      const topic = `user:${user.id}`
      joinChannel(topic)
      addJoinedChannel(topic)
      return () => {
        leaveChannel(topic)
        removeJoinedChannel(topic)
      }
    }
  }, [isConnected, user?.id, joinChannel, leaveChannel, addJoinedChannel, removeJoinedChannel])

  // Auto-join/leave org channel on org switch. Needs the cleanup: the effect
  // re-runs on every reconnect, so without one the join never gets its leave.
  useEffect(() => {
    if (!isConnected || !currentOrg?.id) return

    const topic = `org:${currentOrg.id}`
    joinChannel(topic)
    addJoinedChannel(topic)

    return () => {
      leaveChannel(topic)
      removeJoinedChannel(topic)
    }
  }, [isConnected, currentOrg?.id, joinChannel, leaveChannel, addJoinedChannel, removeJoinedChannel])

  // Connection quality, from the heartbeat roundtrip the socket provider
  // measures. The previous version compared its own 5s interval against
  // itself, so ordinary timer drift read as "degraded" and the header claimed
  // "Slow connection" on a perfectly healthy socket.
  useEffect(() => {
    if (!isConnected) return
    // Null until the first heartbeat reply lands: a connected socket with
    // nothing to report yet, not a slow one.
    if (wsLatencyMs === null) {
      setConnectionQuality('good')
      return
    }
    if (wsLatencyMs > POOR_LATENCY_MS) {
      setConnectionQuality('poor')
    } else if (wsLatencyMs > DEGRADED_LATENCY_MS) {
      setConnectionQuality('degraded')
    } else {
      setConnectionQuality('good')
    }
  }, [isConnected, wsLatencyMs, setConnectionQuality])

  // Seed the unread inbox count from the server. The store value is otherwise
  // session-only (realtime increments it but it starts at 0), so seeding makes
  // the title + favicon badge reflect the real unread count.
  //
  // This is a react-query (not a one-shot fetch) on purpose: /unibox/count is
  // org-scoped, and on a fresh login the bootstrap fires before OrgGate has
  // re-synced the server session — so the first read returns the wrong/empty
  // count. OrgGate's "sync" switch invalidates org-scoped queries on success
  // (root "unibox" included), which refetches this against the correct session
  // and corrects the badge without a reload. Gated on a selected workspace so a
  // multi-org login (which redirects to /select-org before any sync) doesn't
  // fire a NULL-org read. Best-effort: a failure leaves the current count as-is.
  const unseenQuery = useUnseenCount({ enabled: !!currentOrg })
  useEffect(() => {
    const c = unseenQuery.data?.count
    if (typeof c === 'number') setUnseenCount(c)
  }, [unseenQuery.data, setUnseenCount])

  // Set up event-to-store routing
  useRealtimeEvents()

  // Presence rides the same org channel: who's online, who's viewing what.
  return <PresenceProvider>{children}</PresenceProvider>
}
