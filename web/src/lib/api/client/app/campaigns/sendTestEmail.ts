import Request from "../../Request";

export interface SendTestEmailInput {
    // Sending mailbox; any mailbox of the organization.
    account_id: string;
    recipient: string;
    // Step to send; omitted = the first step.
    step_id?: string;
    // Contact to render the copy for; omitted = a placeholder contact.
    contact_id?: string;
}

export interface SendTestEmailResult {
    message: string;
    recipient: string;
    subject: string;
    account_id: string;
    step_id: string;
    contact_id?: string;
}

// POST /campaigns/:id/test-email. Sends the SAVED step through a real worker
// with the campaign's attachments, the mailbox signature and the opt-out
// footer; tracking is off and the opt-out link names nobody.
export default async function sendTestEmail(campaignId: string, input: SendTestEmailInput): Promise<SendTestEmailResult> {
    return await Request<SendTestEmailResult>({
        method: "POST",
        url: `/campaigns/${campaignId}/test-email`,
        data: input,
        authorization: true,
    });
}
