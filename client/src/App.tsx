import { useEffect, useState } from "react";
import { MessageSquareText, PhoneCall, PlusCircle, Webhook } from "lucide-react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { AppShell } from "@/components/layout/AppShell";
import { CallsPage } from "@/pages/CallsPage";
import { MessagesPage } from "@/pages/MessagesPage";
import { WebhooksPage } from "@/pages/WebhooksPage";
import { SessionPairing } from "@/components/domain/session/SessionPairing";
import { SessionHeader } from "@/components/domain/session/SessionHeader";
import { IncomingCallModal } from "@/components/domain/call/IncomingCallModal";
import { TransferOfferModal } from "@/components/domain/call/TransferOfferModal";
import { EmptyState } from "@/components/shared/EmptyState";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import { ensureCallsWired } from "@/stores/calls";
import { useTheme } from "@/stores/theme";

export const App = () => {
  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const theme = useTheme((s) => s.theme);
  const [page, setPage] = useState<"calls" | "messages" | "webhooks">("calls");

  useEffect(() => {
    ensureSessionsWired();
    ensureCallsWired();
  }, []);

  const active = sessions.find((s) => s.id === activeId) ?? null;

  return (
    <TooltipProvider delayDuration={200}>
      <AppShell
        title={page === "calls" ? "Chamadas" : page === "messages" ? "Mensagens" : "Webhooks"}
        navigation={active?.paired ? (
          <div className="flex rounded-full border bg-card p-1">
            <button onClick={() => setPage("calls")} className={`flex items-center gap-2 rounded-full px-3 py-1.5 text-sm ${page === "calls" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}>
              <PhoneCall className="h-4 w-4" /> <span className="hidden sm:inline">Ligações</span>
            </button>
            <button onClick={() => setPage("messages")} className={`flex items-center gap-2 rounded-full px-3 py-1.5 text-sm ${page === "messages" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}>
              <MessageSquareText className="h-4 w-4" /> <span className="hidden sm:inline">Mensagens</span>
            </button>
            <button onClick={() => setPage("webhooks")} className={`flex items-center gap-2 rounded-full px-3 py-1.5 text-sm ${page === "webhooks" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}>
              <Webhook className="h-4 w-4" /> <span className="hidden sm:inline">Webhooks</span>
            </button>
          </div>
        ) : null}
      >
        {sessions.length === 0 ? (
          <EmptyState
            icon={<PlusCircle className="h-6 w-6" />}
            title="Nenhuma conta ainda"
            description="Crie sua primeira conta de WhatsApp na barra lateral para começar a ligar."
          />
        ) : active ? (
          <div className="space-y-6">
            <SessionHeader session={active} />
            {active.paired ? (page === "calls" ? <CallsPage sid={active.id} /> : page === "messages" ? <MessagesPage sid={active.id} /> : <WebhooksPage />) : <SessionPairing session={active} />}
          </div>
        ) : (
          <EmptyState title="Selecione uma conta" description="Escolha uma conta na barra lateral." />
        )}
      </AppShell>
      <IncomingCallModal />
      <TransferOfferModal />
      <Toaster theme={theme} position="top-right" richColors closeButton />
    </TooltipProvider>
  );
};
