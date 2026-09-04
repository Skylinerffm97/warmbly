import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    addSegmentToCampaign,
    listCampaignSegments,
    setCampaignSegments,
    listContactSegments,
    listSegmentOverrides,
    createSegment,
    deleteSegment,
    getSegment,
    listSegmentFields,
    listSegments,
    lookupSegmentMembers,
    previewSegment,
    setSegmentMembers,
    updateSegment,
} from "@/lib/api/client/app/segments";
import type { SegmentMemberMode, SegmentPreview, SegmentWrite } from "@/lib/api/models/app/segments/Segment";

// Every segment read lives under ["segments"]: the realtime spine invalidates
// that prefix on any segment or contact mutation, since membership is live.
export function useSegments(enabled = true) {
    return useQuery({ queryKey: ["segments", "list"], queryFn: listSegments, enabled });
}

export function useSegment(id: string | undefined) {
    return useQuery({ queryKey: ["segments", id], queryFn: () => getSegment(id as string), enabled: !!id });
}

export function useSegmentFields(enabled = true) {
    return useQuery({ queryKey: ["segments", "fields"], queryFn: listSegmentFields, enabled, staleTime: 5 * 60 * 1000 });
}

export function useSegmentPreview(preview: SegmentPreview | null) {
    return useQuery({
        queryKey: ["segments", "preview", preview],
        queryFn: () => previewSegment(preview as SegmentPreview),
        enabled: preview !== null,
        retry: 0,
    });
}

export function useSegmentMemberModes(id: string | undefined, contacts: string[]) {
    return useQuery({
        queryKey: ["segments", id, "members", contacts],
        queryFn: () => lookupSegmentMembers(id as string, contacts),
        enabled: !!id && contacts.length > 0,
    });
}

// Keyed under ["contacts", id] so a contact mutation refreshes it, and under
// ["segments"] via the spine so a segment edit does too.
export function useContactSegments(contactId: string | undefined, enabled = true) {
    return useQuery({
        queryKey: ["contacts", contactId, "segments"],
        queryFn: () => listContactSegments(contactId as string),
        enabled: enabled && !!contactId,
        staleTime: 30_000,
    });
}

export function useSegmentOverrides(id: string | undefined, enabled = true) {
    return useQuery({
        queryKey: ["segments", id, "overrides"],
        queryFn: () => listSegmentOverrides(id as string),
        enabled: enabled && !!id,
    });
}

function invalidateSegments(queryClient: ReturnType<typeof useQueryClient>) {
    return queryClient.invalidateQueries({ queryKey: ["segments"] });
}

export function useCreateSegment() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: SegmentWrite) => createSegment(data),
        onSuccess: () => invalidateSegments(queryClient),
    });
}

export function useUpdateSegment() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: SegmentWrite }) => updateSegment(id, data),
        // A changed definition moves rows in and out of the segment's contact list.
        onSuccess: () =>
            Promise.all([invalidateSegments(queryClient), queryClient.invalidateQueries({ queryKey: ["contacts", "list"] })]),
    });
}

export function useDeleteSegment() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteSegment(id),
        onSuccess: () => invalidateSegments(queryClient),
    });
}

export function useSetSegmentMembers() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, contacts, mode }: { id: string; contacts: string[]; mode: SegmentMemberMode }) =>
            setSegmentMembers(id, contacts, mode),
        // ["contacts"] as a whole: the list moves and each pinned contact's
        // own segments panel changes.
        onSuccess: () =>
            Promise.all([invalidateSegments(queryClient), queryClient.invalidateQueries({ queryKey: ["contacts"] })]),
    });
}

// Keyed under ["segments"]: the counts move with contact and segment writes,
// and the campaign spine refreshes it by id, but send and open events (which
// invalidate ["campaigns", id]) must not recount every linked segment.
export function useCampaignSegments(campaignId: string | undefined, enabled = true) {
    return useQuery({
        queryKey: ["segments", "campaign", campaignId],
        queryFn: () => listCampaignSegments(campaignId as string),
        enabled: enabled && !!campaignId,
    });
}

export function useSetCampaignSegments() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ campaignId, segmentIds }: { campaignId: string; segmentIds: string[] }) =>
            setCampaignSegments(campaignId, segmentIds),
        // Linking enrols leads right away, so the campaign's contact list moves.
        onSuccess: () =>
            Promise.all([
                queryClient.invalidateQueries({ queryKey: ["contacts"] }),
                queryClient.invalidateQueries({ queryKey: ["segments"] }),
            ]),
    });
}

export function useAddSegmentToCampaign() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, campaignId }: { id: string; campaignId: string }) => addSegmentToCampaign(id, campaignId),
        onSuccess: () =>
            Promise.all([
                queryClient.invalidateQueries({ queryKey: ["contacts"] }),
                queryClient.invalidateQueries({ queryKey: ["campaigns"] }),
                queryClient.invalidateQueries({ queryKey: ["segments"] }),
            ]),
    });
}
