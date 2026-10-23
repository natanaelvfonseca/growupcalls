import { useEffect, useMemo, useRef, useState } from "react";
import { Braces, CheckCircle2, FileSpreadsheet, ListChecks, Loader2, MessageSquareText, MousePointerClick, Play, RefreshCw, Send, Trash2, Upload, Users, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cancelCampaign, createCampaign, getCampaign, listCampaignResponses, listCampaignSends, listCampaigns, type CampaignButton, type CampaignJob, type CampaignRecipient, type CampaignResponse, type CampaignSend, type MessageKind } from "@/services/campaigns";
import { cn } from "@/lib/utils";

const textareaClass = "min-h-28 w-full resize-y rounded-xl border border-input bg-transparent px-3 py-2 text-sm shadow-sm outline-none placeholder:text-muted-foreground focus:ring-1 focus:ring-ring";
const normalize = (value: string) => value.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase().trim();
const cleanPhone = (value: string) => value.replace(/\D/g, "");

// Parser leve, sem dependência: respeita aspas, vírgula, ponto-e-vírgula e CRLF.
const parseDelimited = (text: string): string[][] => {
  const first = text.split("\n")[0];
  const delimiter = (first.match(/;/g)?.length ?? 0) >= (first.match(/,/g)?.length ?? 0) ? ";" : ",";
  const rows: string[][] = [];
  let row: string[] = [], cell = "", quoted = false;
  for (let i = 0; i < text.length; i += 1) {
    const char = text[i];
    if (char === '"' && quoted && text[i + 1] === '"') { cell += '"'; i += 1; }
    else if (char === '"') quoted = !quoted;
    else if (char === delimiter && !quoted) { row.push(cell.trim()); cell = ""; }
    else if ((char === "\n" || char === "\r") && !quoted) {
      if (char === "\r" && text[i + 1] === "\n") i += 1;
      row.push(cell.trim()); cell = "";
      if (row.some(Boolean)) rows.push(row);
      row = [];
    } else cell += char;
  }
  row.push(cell.trim());
  if (row.some(Boolean)) rows.push(row);
  return rows;
};

const rowsToRecipients = (rows: string[][]) => {
  if (rows.length < 2) throw new Error("A planilha precisa ter cabeçalho e pelo menos um lead.");
  const headers = rows[0].map((h, i) => normalize(h).replace(/\s+/g, "_") || `campo_${i + 1}`);
  const phoneIndex = headers.findIndex((h) => ["telefone", "phone", "celular", "whatsapp", "numero"].includes(h));
  if (phoneIndex < 0) throw new Error("Crie uma coluna chamada telefone, celular, WhatsApp ou phone.");
  const seen = new Set<string>();
  const recipients = rows.slice(1).map((values) => {
    const variables: Record<string, string> = {};
    headers.forEach((header, i) => { variables[header] = values[i]?.trim() ?? ""; });
    const phone = cleanPhone(values[phoneIndex] ?? "");
    variables.telefone = phone;
    return { phone, variables };
  }).filter((r) => {
    if (r.phone.length < 10 || seen.has(r.phone)) return false;
    seen.add(r.phone);
    return true;
  });
  return { recipients, headers };
};

const kindOptions: Array<{ value: MessageKind; label: string; icon: typeof Send }> = [
  { value: "text", label: "Texto", icon: MessageSquareText },
  { value: "buttons", label: "Respostas", icon: MousePointerClick },
  { value: "interactive", label: "Ações", icon: Play },
  { value: "list", label: "Lista", icon: ListChecks },
];

const statusLabel: Record<CampaignJob["status"], string> = { pending: "Preparando", running: "Enviando", completed: "Concluída", completed_with_failures: "Concluída com falhas", canceled: "Cancelada" };

export const MessagesPage = ({ sid }: { sid: string }) => {
  const inputRef = useRef<HTMLInputElement>(null);
  const [recipients, setRecipients] = useState<CampaignRecipient[]>([]);
  const [headers, setHeaders] = useState<string[]>([]);
  const [fileName, setFileName] = useState("");
  const [campaignName, setCampaignName] = useState("");
  const [kind, setKind] = useState<MessageKind>("text");
  const [body, setBody] = useState("Olá, {{nome}}! ");
  const [variations, setVariations] = useState<string[]>([]);
  const [previewIndex, setPreviewIndex] = useState(0);
  const [footer, setFooter] = useState("Enviado pela Growup");
  const [buttons, setButtons] = useState<CampaignButton[]>([
    { type: "quick_reply", displayText: "Tenho interesse", id: "interesse" },
    { type: "quick_reply", displayText: "Falar depois", id: "depois" },
  ]);
  const [buttonText, setButtonText] = useState("Ver opções");
  const [listMode, setListMode] = useState<"classic" | "native">("classic");
  const [rows, setRows] = useState([
    { id: "opcao_1", title: "Quero saber mais", description: "Receber mais informações" },
    { id: "opcao_2", title: "Falar com atendente", description: "Continuar com uma pessoa" },
  ]);
  const [delaySeconds, setDelaySeconds] = useState(5);
  const [jitterSeconds, setJitterSeconds] = useState(3);
  const [sending, setSending] = useState(false);
  const [jobs, setJobs] = useState<CampaignJob[]>([]);
  const [responses, setResponses] = useState<CampaignResponse[]>([]);
  const [sends, setSends] = useState<CampaignSend[]>([]);

  const preview = useMemo(() => {
    const vars = recipients[0]?.variables ?? { nome: "Maria", telefone: "5511999999999" };
    const templates = [body, ...variations];
    let value = templates[Math.min(previewIndex, templates.length - 1)] ?? body;
    Object.entries(vars).forEach(([key, replacement]) => { value = value.replaceAll(`{{${key}}}`, replacement); });
    return value;
  }, [body, previewIndex, recipients, variations]);

  const loadJobs = async () => {
    try {
      const { campaigns } = await listCampaigns(sid);
      const detailed = await Promise.all(campaigns.slice(-8).map((j) => getCampaign(sid, j.id).catch(() => j)));
      setJobs(detailed.sort((a, b) => b.createdAt - a.createdAt));
    } catch { /* sessão trocando ou backend anterior: não interrompe o editor */ }
  };

  const loadResponses = async () => {
    try { setResponses((await listCampaignResponses(sid)).responses); }
    catch { /* versão anterior durante deploy ou sessão trocando */ }
  };

  const loadSends = async () => {
    try { setSends((await listCampaignSends(sid)).sends); }
    catch { /* versão anterior durante deploy ou sessão trocando */ }
  };

  useEffect(() => {
    void loadJobs();
    void loadResponses();
    void loadSends();
    const timer = window.setInterval(() => { void loadJobs(); void loadResponses(); void loadSends(); }, 3000);
    return () => window.clearInterval(timer);
  }, [sid]);

  const importFile = async (file?: File) => {
    if (!file) return;
    try {
      const parsed = rowsToRecipients(parseDelimited((await file.text()).replace(/^\uFEFF/, "")));
      if (!parsed.recipients.length) throw new Error("Nenhum telefone válido foi encontrado.");
      setRecipients(parsed.recipients); setHeaders(parsed.headers); setFileName(file.name);
      if (!campaignName) setCampaignName(file.name.replace(/\.(csv|txt)$/i, ""));
      toast.success(`${parsed.recipients.length} leads importados`);
    } catch (error) { toast.error((error as Error).message); }
  };

  const addVariable = (name: string) => setBody((current) => `${current}${current.endsWith(" ") || !current ? "" : " "}{{${name}}}`);
  const setButton = (index: number, patch: Partial<CampaignButton>) => setButtons((all) => all.map((b, i) => i === index ? { ...b, ...patch } : b));

  const start = async () => {
    if (!recipients.length) { toast.error("Importe uma lista de leads primeiro."); return; }
    if (!body.trim()) { toast.error("Escreva a mensagem."); return; }
    if ((kind === "buttons" || kind === "interactive") && !buttons.length) { toast.error("Adicione pelo menos um botão."); return; }
    if (kind === "list" && !rows.length) { toast.error("Adicione pelo menos uma opção."); return; }
    setSending(true);
    try {
      const job = await createCampaign(sid, {
        name: campaignName.trim() || `Campanha ${new Date().toLocaleString("pt-BR")}`,
        recipients,
        content: { kind, body, variants: [body, ...variations], listMode, footer, buttonText, buttons, sections: kind === "list" ? [{ title: "Opções", rows }] : undefined },
        delayMs: Math.max(1, delaySeconds) * 1000,
        jitterMs: Math.max(0, jitterSeconds) * 1000,
      });
      setJobs((current) => [job, ...current]);
      toast.success("Campanha iniciada no servidor. Você pode fechar esta aba.");
    } catch (error) { toast.error((error as Error).message); }
    finally { setSending(false); }
  };

  return <div className="space-y-6">
    <div className="grid gap-6 xl:grid-cols-[minmax(0,1.5fr)_minmax(320px,.8fr)]">
      <div className="space-y-6">
        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Users className="h-5 w-5" /> Destinatários</CardTitle><CardDescription>Importe CSV com uma coluna de telefone. As demais colunas viram variáveis.</CardDescription></CardHeader><CardContent className="space-y-4">
          <input ref={inputRef} className="hidden" type="file" accept=".csv,.txt,text/csv" onChange={(e) => void importFile(e.target.files?.[0])} />
          <button type="button" onClick={() => inputRef.current?.click()} onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); void importFile(e.dataTransfer.files[0]); }} className="flex w-full flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-primary/35 bg-primary/[.03] px-5 py-7 text-center transition-colors hover:bg-primary/[.06]">
            {recipients.length ? <FileSpreadsheet className="h-7 w-7 text-primary" /> : <Upload className="h-7 w-7 text-primary" />}<span className="font-medium">{fileName || "Escolher ou arrastar uma planilha CSV"}</span><span className="text-xs text-muted-foreground">{recipients.length ? `${recipients.length} telefones válidos · ${headers.length} colunas` : "Exemplo: nome;telefone;curso;vencimento"}</span>
          </button>
          {headers.length > 0 && <div className="flex flex-wrap gap-2"><span className="py-1 text-xs text-muted-foreground">Variáveis:</span>{headers.filter((h) => h !== "telefone").map((h) => <button type="button" key={h} onClick={() => addVariable(h)} className="rounded-full bg-secondary px-2.5 py-1 font-mono text-xs text-secondary-foreground hover:bg-accent">{`{{${h}}}`}</button>)}</div>}
        </CardContent></Card>

        <Card><CardHeader><CardTitle className="flex items-center gap-2"><Send className="h-5 w-5" /> Montar mensagem</CardTitle><CardDescription>O conteúdo é personalizado para cada linha antes do envio real.</CardDescription></CardHeader><CardContent className="space-y-5">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">{kindOptions.map((option) => <button type="button" key={option.value} onClick={() => setKind(option.value)} className={cn("flex items-center justify-center gap-2 rounded-xl border px-3 py-2.5 text-sm transition-colors", kind === option.value ? "border-primary bg-primary text-primary-foreground" : "bg-card hover:bg-accent")}><option.icon className="h-4 w-4" />{option.label}</button>)}</div>
          <div className="space-y-2"><Label>Nome da campanha</Label><Input value={campaignName} onChange={(e) => setCampaignName(e.target.value)} placeholder="Ex.: Leads outubro" /></div>
          <div className="space-y-2"><Label>Mensagem</Label><textarea className={textareaClass} value={body} onChange={(e) => setBody(e.target.value)} placeholder="Olá, {{nome}}! Temos uma informação para você." /><p className="flex items-center gap-1 text-xs text-muted-foreground"><Braces className="h-3.5 w-3.5" /> Use qualquer coluna como variável: {`{{nome}}`}, {`{{curso}}`}, {`{{vencimento}}`}.</p></div>
          <div className="space-y-3 rounded-2xl border bg-muted/20 p-4">
            <div className="flex flex-wrap items-center justify-between gap-2"><div><Label>Variações da mensagem</Label><p className="mt-1 text-xs text-muted-foreground">O servidor alterna as versões em rodízio entre os contatos.</p></div><Button type="button" variant="outline" size="sm" onClick={() => { setVariations((current) => [...current, ""]); setPreviewIndex(variations.length + 1); }}>Adicionar variação</Button></div>
            {variations.length === 0 ? <p className="text-sm text-muted-foreground">Somente a mensagem principal será usada.</p> : variations.map((variation, index) => <div key={index} className="space-y-2 rounded-xl border bg-card p-3"><div className="flex items-center justify-between"><button type="button" className={cn("text-sm font-medium", previewIndex === index + 1 && "text-primary")} onClick={() => setPreviewIndex(index + 1)}>Variação {index + 2}</button><Button type="button" variant="ghost" size="icon" onClick={() => { setVariations((all) => all.filter((_, i) => i !== index)); setPreviewIndex(0); }}><Trash2 className="h-4 w-4" /></Button></div><textarea className={textareaClass} value={variation} onFocus={() => setPreviewIndex(index + 1)} onChange={(e) => setVariations((all) => all.map((value, i) => i === index ? e.target.value : value))} placeholder={`Outra forma de escrever a mensagem usando {{nome}}...`} /></div>)}
          </div>
          {kind !== "text" && <div className="space-y-2"><Label>Rodapé</Label><Input value={footer} onChange={(e) => setFooter(e.target.value)} /></div>}

          {(kind === "buttons" || kind === "interactive") && <div className="space-y-3"><div className="flex items-center justify-between"><Label>Botões {kind === "buttons" && "(máximo 3)"}</Label><Button type="button" variant="outline" size="sm" disabled={kind === "buttons" && buttons.length >= 3} onClick={() => setButtons((current) => [...current, { type: kind === "buttons" ? "quick_reply" : "cta_url", displayText: "Novo botão", id: `acao_${current.length + 1}` }])}>Adicionar</Button></div>
            {buttons.map((button, index) => <div key={index} className="grid gap-2 rounded-xl border bg-muted/30 p-3 sm:grid-cols-[140px_1fr_1fr_auto]">
              <select className="h-9 rounded-md border bg-card px-2 text-sm" value={kind === "buttons" ? "quick_reply" : button.type} disabled={kind === "buttons"} onChange={(e) => setButton(index, { type: e.target.value as CampaignButton["type"] })}><option value="quick_reply">Resposta</option><option value="cta_url">Abrir link</option><option value="cta_copy">Copiar código</option><option value="cta_call">Ligar</option></select>
              <Input value={button.displayText} onChange={(e) => setButton(index, { displayText: e.target.value })} placeholder="Texto do botão" />
              <Input value={button.type === "cta_url" ? button.url ?? "" : button.type === "cta_copy" ? button.copyCode ?? "" : button.type === "cta_call" ? button.phoneNumber ?? "" : button.id ?? ""} onChange={(e) => setButton(index, button.type === "cta_url" ? { url: e.target.value } : button.type === "cta_copy" ? { copyCode: e.target.value } : button.type === "cta_call" ? { phoneNumber: e.target.value } : { id: e.target.value })} placeholder={button.type === "cta_url" ? "https://..." : button.type === "cta_call" ? "Telefone" : button.type === "cta_copy" ? "Código" : "ID da resposta"} />
              <Button type="button" variant="ghost" size="icon" onClick={() => setButtons((all) => all.filter((_, i) => i !== index))}><Trash2 className="h-4 w-4" /></Button>
            </div>)}</div>}

          {kind === "list" && <div className="space-y-3"><div className="grid gap-4 sm:grid-cols-2"><div className="space-y-2"><Label>Texto do botão da lista</Label><Input value={buttonText} onChange={(e) => setButtonText(e.target.value)} /></div><div className="space-y-2"><Label>Compatibilidade</Label><select className="h-9 w-full rounded-md border bg-card px-3 text-sm" value={listMode} onChange={(e) => setListMode(e.target.value as "classic" | "native")}><option value="classic">Lista clássica (recomendado)</option><option value="native">Lista moderna (native flow)</option></select></div></div><div className="flex items-center justify-between"><Label>Opções</Label><Button type="button" variant="outline" size="sm" onClick={() => setRows((current) => [...current, { id: `opcao_${current.length + 1}`, title: "Nova opção", description: "" }])}>Adicionar</Button></div>
            {rows.map((row, index) => <div key={index} className="grid gap-2 rounded-xl border bg-muted/30 p-3 sm:grid-cols-[1fr_1fr_1.4fr_auto]"><Input value={row.id} onChange={(e) => setRows((all) => all.map((r, i) => i === index ? { ...r, id: e.target.value } : r))} placeholder="ID" /><Input value={row.title} onChange={(e) => setRows((all) => all.map((r, i) => i === index ? { ...r, title: e.target.value } : r))} placeholder="Título" /><Input value={row.description} onChange={(e) => setRows((all) => all.map((r, i) => i === index ? { ...r, description: e.target.value } : r))} placeholder="Descrição" /><Button type="button" variant="ghost" size="icon" onClick={() => setRows((all) => all.filter((_, i) => i !== index))}><Trash2 className="h-4 w-4" /></Button></div>)}</div>}

          <div className="grid gap-4 sm:grid-cols-2"><div className="space-y-2"><Label>Intervalo mínimo (segundos)</Label><Input type="number" min={1} value={delaySeconds} onChange={(e) => setDelaySeconds(Number(e.target.value))} /></div><div className="space-y-2"><Label>Variação aleatória (segundos)</Label><Input type="number" min={0} value={jitterSeconds} onChange={(e) => setJitterSeconds(Number(e.target.value))} /></div></div>
          <div className="flex items-center justify-between gap-4 rounded-xl border bg-amber-500/5 p-4"><p className="text-sm text-muted-foreground">O envio começará imediatamente para <strong className="text-foreground">{recipients.length} leads</strong>. Use apenas contatos autorizados.</p><Button onClick={() => void start()} disabled={sending || !recipients.length}>{sending ? <Loader2 className="animate-spin" /> : <Send />} Iniciar disparo</Button></div>
        </CardContent></Card>
      </div>

      <Card className="h-fit xl:sticky xl:top-0"><CardHeader><CardTitle>Prévia</CardTitle><CardDescription>Exemplo usando o primeiro lead da lista.</CardDescription></CardHeader><CardContent className="space-y-4"><div className="flex flex-wrap gap-2">{[body, ...variations].map((_, index) => <button type="button" key={index} onClick={() => setPreviewIndex(index)} className={cn("rounded-full border px-2.5 py-1 text-xs", previewIndex === index ? "border-primary bg-primary text-primary-foreground" : "bg-card text-muted-foreground")}>Versão {index + 1}</button>)}</div><div className="mx-auto max-w-sm rounded-2xl bg-[#e7ffdb] p-4 text-[15px] text-[#17221a] shadow-sm dark:bg-emerald-950 dark:text-emerald-50"><p className="whitespace-pre-wrap">{preview || "Sua mensagem aparecerá aqui."}</p>{kind === "list" && <button className="mt-3 w-full border-t border-emerald-900/10 pt-3 font-medium text-sky-700">{buttonText}</button>}{(kind === "buttons" || kind === "interactive") && <div className="mt-3 space-y-1 border-t border-emerald-900/10 pt-2">{buttons.map((b, i) => <div key={i} className="py-1.5 text-center font-medium text-sky-700">{b.displayText}</div>)}</div>}{kind !== "text" && footer && <p className="mt-2 text-xs opacity-60">{footer}</p>}</div></CardContent></Card>
    </div>

    <Card><CardHeader className="flex-row items-start justify-between"><div><CardTitle>Mensagens disparadas</CardTitle><CardDescription>Destinatários que receberam as campanhas, inclusive listas recuperadas do histórico.</CardDescription></div><Button type="button" variant="outline" size="sm" onClick={() => void loadSends()}><RefreshCw className="h-4 w-4" /> Atualizar</Button></CardHeader><CardContent>{sends.length === 0 ? <p className="text-sm text-muted-foreground">Nenhum disparo foi encontrado no histórico desta conta.</p> : <div className="overflow-x-auto"><table className="w-full min-w-[760px] text-left text-sm"><thead><tr className="border-b text-xs text-muted-foreground"><th className="px-3 py-2 font-medium">Destinatário</th><th className="px-3 py-2 font-medium">Campanha</th><th className="px-3 py-2 font-medium">Mensagem</th><th className="px-3 py-2 font-medium">Status</th><th className="px-3 py-2 font-medium">Data</th></tr></thead><tbody>{sends.map((send) => <tr key={send.id} className="border-b last:border-0"><td className="px-3 py-3 font-mono text-xs">+{send.recipient.split("@")[0]}</td><td className="px-3 py-3">{send.campaignName || "Campanha"}</td><td className="max-w-md px-3 py-3"><p className="line-clamp-2">{send.body || "Mensagem interativa"}</p></td><td className="px-3 py-3"><span className={cn("rounded-full px-2 py-1 text-xs font-medium", send.status === "sent" ? "bg-emerald-500/10 text-emerald-700" : "bg-destructive/10 text-destructive")}>{send.status === "sent" ? "Enviada" : "Falhou"}</span></td><td className="px-3 py-3 text-muted-foreground">{new Date(send.timestamp).toLocaleString("pt-BR")}</td></tr>)}</tbody></table></div>}</CardContent></Card>

    <Card><CardHeader className="flex-row items-start justify-between"><div><CardTitle>Respostas recebidas</CardTitle><CardDescription>Cliques e mensagens dos contatos, inclusive das campanhas já enviadas.</CardDescription></div><Button type="button" variant="outline" size="sm" onClick={() => void loadResponses()}><RefreshCw className="h-4 w-4" /> Atualizar</Button></CardHeader><CardContent>{responses.length === 0 ? <p className="text-sm text-muted-foreground">Nenhuma resposta foi encontrada no histórico desta conta.</p> : <div className="overflow-x-auto"><table className="w-full min-w-[620px] text-left text-sm"><thead><tr className="border-b text-xs text-muted-foreground"><th className="px-3 py-2 font-medium">Contato</th><th className="px-3 py-2 font-medium">Resposta</th><th className="px-3 py-2 font-medium">Data</th></tr></thead><tbody>{responses.map((response) => <tr key={response.id} className="border-b last:border-0"><td className="px-3 py-3 font-mono text-xs">+{response.chat.split("@")[0]}</td><td className="px-3 py-3 font-medium">{response.body || "Resposta recebida"}</td><td className="px-3 py-3 text-muted-foreground">{new Date(response.timestamp).toLocaleString("pt-BR")}</td></tr>)}</tbody></table></div>}</CardContent></Card>

    <Card><CardHeader><CardTitle>Campanhas recentes</CardTitle><CardDescription>O processamento continua no servidor mesmo com a aba fechada.</CardDescription></CardHeader><CardContent className="space-y-3">{jobs.length === 0 ? <p className="text-sm text-muted-foreground">Nenhuma campanha nesta execução do servidor.</p> : jobs.map((job) => { const progress = job.total ? Math.round(((job.sent + job.failed) / job.total) * 100) : 0; return <div key={job.id} className="rounded-xl border p-4"><div className="flex flex-wrap items-center justify-between gap-3"><div><p className="font-medium">{job.name}</p><p className="text-xs text-muted-foreground">{statusLabel[job.status]} · {job.sent} enviados · {job.failed} falhas · {job.total} total</p></div><div className="flex items-center gap-2">{job.status === "running" ? <Loader2 className="h-4 w-4 animate-spin text-primary" /> : job.status === "completed" ? <CheckCircle2 className="h-4 w-4 text-emerald-600" /> : job.failed ? <XCircle className="h-4 w-4 text-destructive" /> : null}{(job.status === "running" || job.status === "pending") && <Button variant="outline" size="sm" onClick={async () => { await cancelCampaign(sid, job.id); void loadJobs(); }}>Cancelar</Button>}</div></div><div className="mt-3 h-2 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-primary transition-all" style={{ width: `${progress}%` }} /></div>{job.results?.some((r) => !r.ok) && <details className="mt-3 text-xs text-destructive"><summary className="cursor-pointer">Ver falhas</summary><div className="mt-2 space-y-1">{job.results.filter((r) => !r.ok).map((r, i) => <p key={i}>{r.to}: {r.error}</p>)}</div></details>}</div>; })}</CardContent></Card>
  </div>;
};
