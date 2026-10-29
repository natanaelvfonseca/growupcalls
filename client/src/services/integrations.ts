import { apiGet, apiPost, apiPut } from "@/lib/api";
import type { CampaignContent, MessageKind } from "@/services/campaigns";

export interface MessageTemplate {
  id: string;
  name: string;
  content: CampaignContent;
  delayMs: number;
  jitterMs: number;
  active: boolean;
  createdAt: number;
  updatedAt: number;
}

export interface IntegrationDispatch {
  source: string;
  eventId: string;
  templateId: string;
  status: "processing" | "accepted" | "failed";
  recipientCount: number;
  jobs: Array<{ sessionId: string; campaignId: string; recipients: number }>;
  error?: string;
  createdAt: number;
  updatedAt: number;
}

export type TemplateInput = {
  name: string;
  active: boolean;
  delayMs: number;
  jitterMs: number;
  content: {
    kind: MessageKind;
    body: string;
    variants: string[];
  };
};

export const listMessageTemplates = () =>
  apiGet<{ templates: MessageTemplate[] }>("/api/message-templates");

export const saveMessageTemplate = (id: string, input: TemplateInput) =>
  apiPut<MessageTemplate>(`/api/message-templates/${encodeURIComponent(id)}`, input);

export const listJovemAprendizDispatches = () =>
  apiGet<{ dispatches: IntegrationDispatch[] }>("/api/integrations/jovem-aprendiz/dispatches?limit=100");

export const sendJovemAprendizTest = (payload: unknown) =>
  apiPost<{ duplicate: boolean; dispatch: IntegrationDispatch }>("/api/integrations/jovem-aprendiz/webhook", payload);
