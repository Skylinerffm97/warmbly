import { useMutation } from "@tanstack/react-query";
import sendTestEmail, { type SendTestEmailInput } from "@/lib/api/client/app/campaigns/sendTestEmail";

export default function useSendTestEmail(campaignId: string) {
    return useMutation({
        mutationFn: (input: SendTestEmailInput) => sendTestEmail(campaignId, input),
    });
}
