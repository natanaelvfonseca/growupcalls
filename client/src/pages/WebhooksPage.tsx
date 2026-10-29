import { useEffect, useMemo, useState } from "react";
import { Check, Clipboard, Code2, Loader2, Pencil, Plus, RefreshCw, Save, Webhook } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { getApiBase } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { listJovemAprendizDispatches, listMessageTemplates, saveMessageTemplate, sendJovemAprendizTest, type IntegrationDispatch, type MessageTemplate } from "@/services/integrations";

const textareaClass = "min-h-28 w-full resize-y rounded-xl border border-input bg-transparent px-3 py-2 text-sm shadow-sm outline-none placeholder:text-muted-foreground focus:ring-1 focus:ring-ring";
const emptyForm = { id: "", name: "", body: "", variants: [] as string[], delaySeconds: 5, jitterSeconds: 2, active: true };

const dispatchStatus: Record<IntegrationDispatch["status"], { label: string; className: string }> = {
  processing: { label: "Processando", className: "bg-amber-500/10 text-amber-700" },
  accepted: { label: "Aceito", className: "bg-emerald-500/10 text-emerald-700" },
  failed: { label: "Falhou", className: "bg-destructive/10 text-destructive" },
};

export const WebhooksPage = () => {
  const [templates, setTemplates] = useState<MessageTemplate[]>([]);
  const [dispatches, setDispatches] = useState<IntegrationDispatch[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [copied, setCopied] = useState<"url" | "payload" | null>(null);
	const [testForm, setTestForm] = useState({ studentName: "Aluno Teste", studentPhone: "", guardianName: "Responsável Teste", guardianPhone: "", templateId: "prova-concluida" });
	const [testing, setTesting] = useState(false);

  const base = getApiBase() || window.location.origin;
  const webhookUrl = `${base}/api/integrations/jovem-aprendiz/webhook`;
  const payload = useMemo(() => JSON.stringify({
    event_id: "matricula-84721",
    template_id: form.id || templates[0]?.id || "primeiro-contato",
    aluno: {
      nome: "Ana",
      programa: "Jovem Aprendiz",
      telefone: "5547999991111",
      telefone_2: "5547999992222",
    },
  }, null, 2), [form.id, templates]);

  const load = async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      const [templateResult, dispatchResult] = await Promise.all([
        listMessageTemplates(),
        listJovemAprendizDispatches(),
      ]);
      setTemplates(templateResult.templates);
      setDispatches(dispatchResult.dispatches);
    } catch (error) {
      if (!quiet) toast.error(`Não foi possível carregar: ${(error as Error).message}`);
    } finally {
      if (!quiet) setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, []);

  const editTemplate = (template: MessageTemplate) => setForm({
    id: template.id,
    name: template.name,
    body: template.content.body || template.content.variants?.[0] || "",
    variants: (template.content.variants ?? []).filter((item) => item !== template.content.body),
    delaySeconds: Math.max(1, Math.round(template.delayMs / 1000)),
    jitterSeconds: Math.max(0, Math.round(template.jitterMs / 1000)),
    active: template.active,
  });

  const save = async () => {
    const id = form.id.trim().toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
    if (!id || !form.name.trim() || !form.body.trim()) {
      toast.error("Preencha o identificador, o nome e a mensagem principal.");
      return;
    }
    setSaving(true);
    try {
      const saved = await saveMessageTemplate(id, {
        name: form.name.trim(),
        active: form.active,
        delayMs: Math.max(1, form.delaySeconds) * 1000,
        jitterMs: Math.max(0, form.jitterSeconds) * 1000,
        content: {
          kind: "text",
          body: form.body.trim(),
          variants: [form.body, ...form.variants].map((value) => value.trim()).filter(Boolean),
        },
      });
      setTemplates((current) => [saved, ...current.filter((item) => item.id !== saved.id)].sort((a, b) => a.name.localeCompare(b.name)));
      setForm({ ...form, id: saved.id });
      toast.success("Modelo salvo e pronto para o webhook.");
    } catch (error) {
      toast.error(`Não foi possível salvar: ${(error as Error).message}`);
    } finally {
      setSaving(false);
    }
  };

  const copy = async (kind: "url" | "payload", value: string) => {
    await navigator.clipboard.writeText(value);
    setCopied(kind);
    window.setTimeout(() => setCopied(null), 1500);
    toast.success(kind === "url" ? "URL copiada" : "Exemplo copiado");
  };

	const sendTest = async () => {
		if (!testForm.studentPhone.trim() || !testForm.guardianPhone.trim()) {
			toast.error("Informe os dois telefones de teste.");
			return;
		}
		if (!templates.some((item) => item.id === testForm.templateId && item.active)) {
			toast.error("Crie e ative o modelo selecionado antes do teste.");
			return;
		}
		setTesting(true);
		try {
			const result = await sendJovemAprendizTest({
				event_id: `teste-painel-${Date.now()}`,
				type: "exam.completed",
				data: { template_id: testForm.templateId, score_percent: 80 },
				student: {
					name: testForm.studentName,
					phone: testForm.studentPhone,
					guardian: { name: testForm.guardianName, phone: testForm.guardianPhone },
				},
			});
			toast.success(`Evento aceito para ${result.dispatch.recipientCount} destinatário(s).`);
			await load(true);
		} catch (error) {
			toast.error(`Teste não enviado: ${(error as Error).message}`);
		} finally {
			setTesting(false);
		}
	};

  if (loading) return <div className="flex min-h-64 items-center justify-center"><Loader2 className="h-6 w-6 animate-spin text-primary" /></div>;

  return <div className="space-y-6">
    <Card>
      <CardHeader><CardTitle className="flex items-center gap-2"><Webhook className="h-5 w-5" /> Webhook do Jovem Aprendiz</CardTitle><CardDescription>O outro sistema envia os dados do aluno para esta URL. O GrowupCalls escolhe o modelo, alterna os números conectados e evita disparos duplicados pelo event_id.</CardDescription></CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2"><Label>URL para receber o webhook</Label><div className="flex gap-2"><Input readOnly value={webhookUrl} className="font-mono text-xs" /><Button variant="outline" onClick={() => void copy("url", webhookUrl)}>{copied === "url" ? <Check /> : <Clipboard />} Copiar</Button></div></div>
        <div className="grid gap-4 md:grid-cols-3">
          <div className="rounded-xl border p-4"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Método</p><p className="mt-2 font-mono font-semibold">POST</p></div>
          <div className="rounded-xl border p-4"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Autenticação</p><p className="mt-2 font-mono text-sm">X-API-Key</p><p className="mt-1 text-xs text-muted-foreground">Use a mesma chave configurada neste painel.</p></div>
          <div className="rounded-xl border p-4"><p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Telefones</p><p className="mt-2 text-sm font-medium">telefone + telefone_2</p><p className="mt-1 text-xs text-muted-foreground">Cada número recebe uma mensagem separada.</p></div>
        </div>
        <details className="rounded-xl border bg-muted/20 p-4"><summary className="cursor-pointer font-medium">Ver exemplo do JSON</summary><div className="mt-3 flex items-start gap-2"><pre className="min-w-0 flex-1 overflow-x-auto rounded-xl bg-zinc-950 p-4 text-xs text-zinc-100">{payload}</pre><Button size="icon" variant="outline" onClick={() => void copy("payload", payload)} aria-label="Copiar JSON">{copied === "payload" ? <Check /> : <Clipboard />}</Button></div></details>
      </CardContent>
    </Card>

		<Card>
			<CardHeader><CardTitle>Testar evento da prova</CardTitle><CardDescription>Simula exatamente o evento exam.completed com aluno e responsável. As mensagens são reais e usam o modelo selecionado.</CardDescription></CardHeader>
			<CardContent className="space-y-4">
				<div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
					<div className="space-y-2"><Label>Nome do aluno</Label><Input value={testForm.studentName} onChange={(e) => setTestForm({ ...testForm, studentName: e.target.value })} /></div>
					<div className="space-y-2"><Label>WhatsApp do aluno</Label><Input value={testForm.studentPhone} onChange={(e) => setTestForm({ ...testForm, studentPhone: e.target.value })} placeholder="5512999999999" /></div>
					<div className="space-y-2"><Label>Nome do responsável</Label><Input value={testForm.guardianName} onChange={(e) => setTestForm({ ...testForm, guardianName: e.target.value })} /></div>
					<div className="space-y-2"><Label>WhatsApp do responsável</Label><Input value={testForm.guardianPhone} onChange={(e) => setTestForm({ ...testForm, guardianPhone: e.target.value })} placeholder="5512988888888" /></div>
				</div>
				<div className="flex flex-wrap items-end justify-between gap-3"><div className="min-w-64 space-y-2"><Label>Modelo</Label><select className="h-9 w-full rounded-xl border border-input bg-card px-3 text-sm" value={testForm.templateId} onChange={(e) => setTestForm({ ...testForm, templateId: e.target.value })}><option value="prova-concluida">prova-concluida</option>{templates.filter((item) => item.id !== "prova-concluida").map((item) => <option key={item.id} value={item.id}>{item.name} ({item.id})</option>)}</select></div><Button onClick={() => void sendTest()} disabled={testing}>{testing ? <Loader2 className="animate-spin" /> : <Webhook />} Enviar teste real</Button></div>
			</CardContent>
		</Card>

    <div className="grid gap-6 xl:grid-cols-[minmax(0,1.25fr)_minmax(320px,.75fr)]">
      <Card>
        <CardHeader><CardTitle className="flex items-center gap-2"><Code2 className="h-5 w-5" /> Modelo de mensagem</CardTitle><CardDescription>Crie textos com variáveis. O sistema alterna as variações entre os destinatários.</CardDescription></CardHeader>
        <CardContent className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-2"><Label>Identificador do modelo</Label><Input value={form.id} onChange={(e) => setForm({ ...form, id: e.target.value })} placeholder="primeiro-contato" /><p className="text-xs text-muted-foreground">É o template_id enviado pelo webhook.</p></div><div className="space-y-2"><Label>Nome</Label><Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="Primeiro contato" /></div></div>
          <div className="space-y-2"><Label>Mensagem principal</Label><textarea className={textareaClass} value={form.body} onChange={(e) => setForm({ ...form, body: e.target.value })} placeholder="Olá {{nome}}, podemos falar sobre o programa {{programa}}?" /><p className="text-xs text-muted-foreground">Variáveis disponíveis são os campos recebidos no webhook, por exemplo: {`{{nome}}`}, {`{{programa}}`} e {`{{curso}}`}.</p></div>
          <div className="space-y-3"><div className="flex items-center justify-between"><div><Label>Variações</Label><p className="mt-1 text-xs text-muted-foreground">Mensagens diferentes reduzem repetição nos disparos.</p></div><Button variant="outline" size="sm" onClick={() => setForm({ ...form, variants: [...form.variants, ""] })}><Plus /> Variação</Button></div>{form.variants.map((variant, index) => <div key={index} className="flex items-start gap-2"><textarea className={cn(textareaClass, "min-h-20")} value={variant} onChange={(e) => setForm({ ...form, variants: form.variants.map((item, i) => i === index ? e.target.value : item) })} placeholder={`Variação ${index + 1}`} /><Button variant="ghost" size="sm" onClick={() => setForm({ ...form, variants: form.variants.filter((_, i) => i !== index) })}>Remover</Button></div>)}</div>
          <div className="grid gap-4 sm:grid-cols-3"><div className="space-y-2"><Label>Intervalo mínimo (s)</Label><Input type="number" min={1} value={form.delaySeconds} onChange={(e) => setForm({ ...form, delaySeconds: Number(e.target.value) })} /></div><div className="space-y-2"><Label>Variação aleatória (s)</Label><Input type="number" min={0} max={60} value={form.jitterSeconds} onChange={(e) => setForm({ ...form, jitterSeconds: Number(e.target.value) })} /></div><div className="flex items-center gap-3 rounded-xl border px-4 py-3"><Switch checked={form.active} onCheckedChange={(active) => setForm({ ...form, active })} aria-label="Modelo ativo" /><div><p className="text-sm font-medium">Modelo ativo</p><p className="text-xs text-muted-foreground">Pode receber envios</p></div></div></div>
          <div className="flex flex-wrap justify-end gap-2"><Button variant="outline" onClick={() => setForm(emptyForm)}><Plus /> Novo</Button><Button onClick={() => void save()} disabled={saving}>{saving ? <Loader2 className="animate-spin" /> : <Save />} Salvar modelo</Button></div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>Modelos salvos</CardTitle><CardDescription>{templates.length} modelo(s) disponível(is) para os webhooks.</CardDescription></CardHeader>
        <CardContent className="space-y-3">{templates.length === 0 ? <p className="text-sm text-muted-foreground">Nenhum modelo criado. Preencha o formulário ao lado para começar.</p> : templates.map((template) => <button key={template.id} type="button" onClick={() => editTemplate(template)} className="w-full rounded-xl border p-4 text-left transition-colors hover:bg-accent"><div className="flex items-start justify-between gap-3"><div><p className="font-medium">{template.name}</p><p className="mt-1 font-mono text-xs text-muted-foreground">{template.id}</p></div><div className="flex items-center gap-2"><Badge variant={template.active ? "default" : "secondary"}>{template.active ? "Ativo" : "Pausado"}</Badge><Pencil className="h-4 w-4 text-muted-foreground" /></div></div><p className="mt-3 line-clamp-2 text-sm text-muted-foreground">{template.content.body || template.content.variants?.[0]}</p><p className="mt-2 text-xs text-muted-foreground">Intervalo {Math.round(template.delayMs / 1000)}s + até {Math.round(template.jitterMs / 1000)}s</p></button>)}</CardContent>
      </Card>
    </div>

    <Card>
      <CardHeader className="flex-row items-start justify-between"><div><CardTitle>Eventos recebidos</CardTitle><CardDescription>Histórico persistente dos webhooks, incluindo dois telefones e campanhas criadas.</CardDescription></div><Button variant="outline" size="sm" onClick={() => void load()}><RefreshCw /> Atualizar</Button></CardHeader>
      <CardContent>{dispatches.length === 0 ? <p className="text-sm text-muted-foreground">Nenhum webhook recebido ainda.</p> : <div className="overflow-x-auto"><table className="w-full min-w-[760px] text-left text-sm"><thead><tr className="border-b text-xs text-muted-foreground"><th className="px-3 py-2 font-medium">Evento</th><th className="px-3 py-2 font-medium">Modelo</th><th className="px-3 py-2 font-medium">Destinatários</th><th className="px-3 py-2 font-medium">Status</th><th className="px-3 py-2 font-medium">Recebido em</th></tr></thead><tbody>{dispatches.map((dispatch) => { const status = dispatchStatus[dispatch.status]; return <tr key={`${dispatch.source}:${dispatch.eventId}`} className="border-b last:border-0"><td className="px-3 py-3 font-mono text-xs">{dispatch.eventId}</td><td className="px-3 py-3">{dispatch.templateId}</td><td className="px-3 py-3">{dispatch.recipientCount}</td><td className="px-3 py-3"><span className={cn("rounded-full px-2 py-1 text-xs font-medium", status.className)}>{status.label}</span>{dispatch.error && <p className="mt-1 text-xs text-destructive">{dispatch.error}</p>}</td><td className="px-3 py-3 text-muted-foreground">{new Date(dispatch.createdAt).toLocaleString("pt-BR")}</td></tr>; })}</tbody></table></div>}</CardContent>
    </Card>
  </div>;
};
