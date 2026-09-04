import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import getCampaigns from "@/lib/api/client/app/campaigns/getCampaigns";
import { DEFAULT_PAGINATION_LIMIT } from "@/lib/information";
import type GetCampaigns from "@/lib/api/models/app/campaigns/GetCampaigns";
import useRealtimeFallbackInterval from "@/hooks/useRealtimeFallback";

interface UseCampaignsProps {
    query: string;
    folder: string;
    limit?: number;
    enabled?: boolean;
}

export default function useCampaigns({ query, folder, limit = DEFAULT_PAGINATION_LIMIT, enabled = true }: UseCampaignsProps) {
    // Send counts on these cards move on realtime invalidation, so the long
    // staleTime below is free while the socket is up and strands the list for
    // five minutes when it is not. Poll only in that second case.
    const refetchInterval = useRealtimeFallbackInterval(enabled);
    const queryResult = useInfiniteQuery<
        GetCampaigns,
        Error,
        InfiniteData<GetCampaigns, string | null>,
        [string, string, string, string, number],
        string | null
    >({
        queryKey: ["campaigns", "list", query, folder, limit],
        queryFn: async ({ pageParam }) => getCampaigns(query, pageParam, folder, limit),
        initialPageParam: null,
        getNextPageParam: (lastPage) => {
            if (lastPage.pagination.has_more) {
                return lastPage.pagination.next_cursor
            }
            return undefined
        },
        staleTime: 5 * 60 * 1000,
        gcTime: 10 * 60 * 1000,
        refetchInterval,
        enabled,
    });

    // Defensive: backend may return `data: null` on empty result sets if
    // the underlying slice was nil. Coerce + drop nulls so consumers can
    // safely read fields without optional-chaining every access.
    const campaigns =
        queryResult.data?.pages
            .flatMap((p) => p.data ?? [])
            .filter((c): c is NonNullable<typeof c> => c != null) ?? [];

    return {
        ...queryResult,
        campaigns,
    };
}
