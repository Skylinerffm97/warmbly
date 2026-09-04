import Request from "../../Request";

export interface TemplatePreviewFrom {
    name: string;
    email: string;
}

export interface TemplatePreviewAttachment {
    id: string;
    filename: string;
    size: number;
    mime_type: string;
}

export interface TemplatePreview {
    subject: string;
    body_html: string;
    body_plain: string;
    errors?: string[]; // template parse errors (these block sending)
    unresolved?: string[]; // literal {{…}} tokens left after render
    // Present when account_id was given: the sender as recipients see it.
    from?: TemplatePreviewFrom;
    // Present when campaign_id was given and the campaign has files attached.
    attachments?: TemplatePreviewAttachment[];
}

export interface TemplatePreviewInput {
    subject: string;
    body_html: string;
    body_plain: string;
    // A contact of the organization to render for; omitted = the built-in sample.
    contact_id?: string;
    // Applies the campaign's opt-out footer and plain-text rule and lists its attachments.
    campaign_id?: string;
    // Applies that mailbox's signature and reports it as the sender.
    account_id?: string;
}

// Renders a campaign template exactly as the send path does (Go template +
// spintax, then signature and opt-out footer when the campaign and mailbox are
// given), returning the output + parse errors + tokens that didn't resolve.
export async function previewTemplate(input: TemplatePreviewInput): Promise<TemplatePreview> {
    return await Request<TemplatePreview>({
        method: "POST",
        url: "/campaign-template-preview",
        data: input,
        authorization: true,
    });
}
