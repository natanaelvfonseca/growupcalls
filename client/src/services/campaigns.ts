import { apiGet, apiPost } from "@/lib/api";

export type MessageKind = "text" | "buttons" | "interactive" | "list";

export interface CampaignRecipient {
  phone: string;
  variables: Record<string, string>;
}

export interface CampaignButton {
  type: "quick_reply" | "cta_url" | "cta_copy" | "cta_call";
  displayText: string;
  id?: string;
  url?: string;
  copyCode?: string;
  phoneNumber?: string;
}

export interface CampaignContent {
  kind: MessageKind;
  body: string;
  variants?: string[];
  footer?: string;
  buttonText?: string;
  buttons?: CampaignButton[];
  sections?: Array<{
    title: string;
    rows: Array<{ id: string; title: string; description?: string }>;
  }>;
}

export interface CampaignJob {
  id: string;
  name: string;
  kind: MessageKind;
  status: "pending" | "running" | "completed" | "completed_with_failures" | "canceled";
  total: number;
  sent: number;
  failed: number;
  createdAt: number;
  results?: Array<{ to: string; ok: boolean; msgId?: string; error?: string }>;
}

export const createCampaign = (
  sid: string,
  payload: {
    name: string;
    recipients: CampaignRecipient[];
    content: CampaignContent;
    delayMs: number;
    jitterMs: number;
  },
) => apiPost<CampaignJob>(`/api/sessions/${sid}/campaigns`, payload);

export const listCampaigns = (sid: string) =>
  apiGet<{ campaigns: CampaignJob[] }>(`/api/sessions/${sid}/campaigns`);

export const getCampaign = (sid: string, id: string) =>
  apiGet<CampaignJob>(`/api/sessions/${sid}/campaigns/${id}`);

export const cancelCampaign = (sid: string, id: string) =>
  apiPost<{ status: string }>(`/api/sessions/${sid}/campaigns/${id}/cancel`, {});
