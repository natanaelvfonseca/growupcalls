package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// CampaignRecipient representa uma linha importada no painel. Variables aceita
// qualquer coluna do CSV; {{nome}}, {{curso}}, etc. são substituídas no servidor.
type CampaignRecipient struct {
	Phone     string            `json:"phone"`
	Variables map[string]string `json:"variables"`
}

type CampaignButton struct {
	Type        string `json:"type"`
	DisplayText string `json:"displayText"`
	ID          string `json:"id"`
	URL         string `json:"url"`
	CopyCode    string `json:"copyCode"`
	PhoneNumber string `json:"phoneNumber"`
}

type CampaignRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type CampaignSection struct {
	Title string        `json:"title"`
	Rows  []CampaignRow `json:"rows"`
}

type CampaignContent struct {
	Kind       string            `json:"kind"` // text|buttons|interactive|list
	Body       string            `json:"body"`
	Variants   []string          `json:"variants,omitempty"`
	Footer     string            `json:"footer"`
	ButtonText string            `json:"buttonText"`
	Buttons    []CampaignButton  `json:"buttons"`
	Sections   []CampaignSection `json:"sections"`
}

type campaignRequest struct {
	Name       string              `json:"name"`
	Recipients []CampaignRecipient `json:"recipients"`
	Content    CampaignContent     `json:"content"`
	DelayMs    int                 `json:"delayMs"`
	JitterMs   int                 `json:"jitterMs"`
}

type campaignJob struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionId"`
	Name      string        `json:"name"`
	Kind      string        `json:"kind"`
	Status    string        `json:"status"`
	Total     int           `json:"total"`
	Sent      int           `json:"sent"`
	Failed    int           `json:"failed"`
	CreatedAt int64         `json:"createdAt"`
	Results   []blastResult `json:"results,omitempty"`
	cancel    chan struct{} `json:"-"`
}

type campaignManager struct {
	mu   sync.Mutex
	jobs map[string]*campaignJob
}

var campaigns = &campaignManager{jobs: map[string]*campaignJob{}}

func renderCampaignTemplate(v string, vars map[string]string) string {
	for key, value := range vars {
		v = strings.ReplaceAll(v, "{{"+strings.TrimSpace(key)+"}}", value)
	}
	return v
}

func renderCampaignContent(src CampaignContent, vars map[string]string) CampaignContent {
	dst := src
	dst.Body = renderCampaignTemplate(src.Body, vars)
	dst.Variants = make([]string, 0, len(src.Variants))
	for _, variant := range src.Variants {
		if rendered := strings.TrimSpace(renderCampaignTemplate(variant, vars)); rendered != "" {
			dst.Variants = append(dst.Variants, rendered)
		}
	}
	dst.Footer = renderCampaignTemplate(src.Footer, vars)
	dst.ButtonText = renderCampaignTemplate(src.ButtonText, vars)
	dst.Buttons = append([]CampaignButton(nil), src.Buttons...)
	for i := range dst.Buttons {
		b := &dst.Buttons[i]
		b.DisplayText = renderCampaignTemplate(b.DisplayText, vars)
		b.ID = renderCampaignTemplate(b.ID, vars)
		b.URL = renderCampaignTemplate(b.URL, vars)
		b.CopyCode = renderCampaignTemplate(b.CopyCode, vars)
		b.PhoneNumber = renderCampaignTemplate(b.PhoneNumber, vars)
	}
	dst.Sections = make([]CampaignSection, len(src.Sections))
	for i, sec := range src.Sections {
		dst.Sections[i].Title = renderCampaignTemplate(sec.Title, vars)
		dst.Sections[i].Rows = append([]CampaignRow(nil), sec.Rows...)
		for j := range dst.Sections[i].Rows {
			r := &dst.Sections[i].Rows[j]
			r.ID = renderCampaignTemplate(r.ID, vars)
			r.Title = renderCampaignTemplate(r.Title, vars)
			r.Description = renderCampaignTemplate(r.Description, vars)
		}
	}
	return dst
}

func buildCampaignMessage(sess *Session, jid types.JID, c CampaignContent) (*waE2E.Message, bool, error) {
	switch c.Kind {
	case "text":
		if strings.TrimSpace(c.Body) == "" {
			return nil, false, errors.New("mensagem vazia")
		}
		return &waE2E.Message{Conversation: proto.String(c.Body)}, false, nil
	case "buttons":
		if len(c.Buttons) == 0 || len(c.Buttons) > 3 {
			return nil, false, errors.New("botões de resposta exigem de 1 a 3 opções")
		}
		btns := make([]*waE2E.ButtonsMessage_Button, 0, len(c.Buttons))
		for i, b := range c.Buttons {
			id := b.ID
			if id == "" {
				id = "btn_" + itoa(i+1)
			}
			typ := waE2E.ButtonsMessage_Button_RESPONSE
			btns = append(btns, &waE2E.ButtonsMessage_Button{ButtonID: proto.String(id), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String(b.DisplayText)}, Type: &typ})
		}
		ht := waE2E.ButtonsMessage_EMPTY
		return &waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{ContentText: proto.String(c.Body), FooterText: proto.String(c.Footer), HeaderType: &ht, Buttons: btns}}}, MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: newMessageSecret()}}, true, nil
	case "interactive":
		if len(c.Buttons) == 0 {
			return nil, false, errors.New("adicione ao menos um botão de ação")
		}
		isGroup := jid.Server == types.GroupServer
		btns := make([]*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton, 0, len(c.Buttons))
		for i, b := range c.Buttons {
			name, params := sess.groupSafeButton(isGroup, b.Type, b.DisplayText, b.URL, b.ID, b.CopyCode, b.PhoneNumber, i)
			if name == "" {
				return nil, false, errors.New("tipo de botão inválido")
			}
			btns = append(btns, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{Name: proto.String(name), ButtonParamsJSON: proto.String(params)})
		}
		return campaignInteractive(c.Body, c.Footer, btns), true, nil
	case "list":
		if len(c.Sections) == 0 {
			return nil, false, errors.New("adicione ao menos uma seção na lista")
		}
		type row struct {
			Header      string `json:"header"`
			Title       string `json:"title"`
			Description string `json:"description"`
			ID          string `json:"id"`
		}
		type section struct {
			Title string `json:"title"`
			Rows  []row  `json:"rows"`
		}
		sections := make([]section, 0, len(c.Sections))
		for _, sec := range c.Sections {
			rows := make([]row, 0, len(sec.Rows))
			for i, r := range sec.Rows {
				id := r.ID
				if id == "" {
					id = "row_" + itoa(i+1)
				}
				rows = append(rows, row{Title: r.Title, Description: r.Description, ID: id})
			}
			sections = append(sections, section{Title: sec.Title, Rows: rows})
		}
		label := c.ButtonText
		if label == "" {
			label = "Ver opções"
		}
		params, _ := json.Marshal(map[string]any{"title": label, "sections": sections})
		btns := []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{Name: proto.String("single_select"), ButtonParamsJSON: proto.String(string(params))}}
		return campaignInteractive(c.Body, c.Footer, btns), true, nil
	default:
		return nil, false, errors.New("tipo de mensagem inválido")
	}
}

func campaignInteractive(body, footer string, btns []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton) *waE2E.Message {
	return &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)}, Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}, InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{Buttons: btns, MessageParamsJSON: proto.String(""), MessageVersion: proto.Int32(1)}}}, MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: newMessageSecret()}}
}

func (m *campaignManager) start(sess *Session, req campaignRequest) *campaignJob {
	job := &campaignJob{ID: whNewID(), SessionID: sess.id, Name: req.Name, Kind: req.Content.Kind, Status: "pending", Total: len(req.Recipients), CreatedAt: time.Now().UnixMilli(), cancel: make(chan struct{})}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	go m.run(sess, job, req)
	return job
}

func (m *campaignManager) run(sess *Session, job *campaignJob, req campaignRequest) {
	m.setStatus(job, "running")
	for i, recipient := range req.Recipients {
		select {
		case <-job.cancel:
			m.setStatus(job, "canceled")
			return
		default:
		}
		if i > 0 {
			delay := req.DelayMs
			if delay < 1000 {
				delay = 3000
			}
			if req.JitterMs > 0 {
				delay += rand.Intn(req.JitterMs + 1)
			}
			select {
			case <-job.cancel:
				m.setStatus(job, "canceled")
				return
			case <-time.After(time.Duration(delay) * time.Millisecond):
			}
		}
		result := blastResult{To: recipient.Phone}
		jid, err := resolveRecipient(recipient.Phone)
		if err == nil {
			content := renderCampaignContent(req.Content, recipient.Variables)
			// Distribuição previsível e equilibrada: cada variação é usada em rodízio.
			// Isso facilita auditoria e garante que nenhuma opção fique sem uso.
			if len(content.Variants) > 0 {
				content.Body = content.Variants[i%len(content.Variants)]
			}
			var msg *waE2E.Message
			var interactive bool
			msg, interactive, err = buildCampaignMessage(sess, jid, content)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				if interactive {
					nodes := []waBinary.Node{bizNativeFlowNode()}
					resp, sendErr := sess.sendResolvingLID(ctx, jid, msg, whatsmeow.SendRequestExtra{AdditionalNodes: &nodes})
					if sendErr == nil {
						sess.recordOutgoing(jid, resp.ID, resp.Timestamp.UnixMilli(), msg)
						result.OK = true
						result.MsgID = resp.ID
					} else {
						err = sendErr
					}
				} else {
					result.MsgID, err = sess.sendAndMark(ctx, jid, msg)
					result.OK = err == nil
				}
				cancel()
			}
		}
		if err != nil {
			result.Error = err.Error()
		}
		m.mu.Lock()
		job.Results = append(job.Results, result)
		if result.OK {
			job.Sent++
		} else {
			job.Failed++
		}
		m.mu.Unlock()
	}
	if job.Failed > 0 {
		m.setStatus(job, "completed_with_failures")
	} else {
		m.setStatus(job, "completed")
	}
}

func (m *campaignManager) setStatus(j *campaignJob, status string) {
	m.mu.Lock()
	j.Status = status
	m.mu.Unlock()
}
func (m *campaignManager) snapshot(j *campaignJob, results bool) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]any{"id": j.ID, "name": j.Name, "kind": j.Kind, "status": j.Status, "total": j.Total, "sent": j.Sent, "failed": j.Failed, "createdAt": j.CreatedAt}
	if results {
		out["results"] = append([]blastResult(nil), j.Results...)
	}
	return out
}

func (s *server) handleCampaignCreate(w http.ResponseWriter, r *http.Request) {
	sess := s.pairedSession(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	var req campaignRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Recipients) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "lista de destinatários obrigatória"})
		return
	}
	if len(req.Recipients) > 10000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limite de 10.000 destinatários por campanha"})
		return
	}
	for i := range req.Recipients {
		req.Recipients[i].Phone = strings.TrimSpace(req.Recipients[i].Phone)
		if req.Recipients[i].Phone == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "telefone vazio na linha " + itoa(i+1)})
			return
		}
	}
	if req.DelayMs < 1000 {
		req.DelayMs = 3000
	}
	job := campaigns.start(sess, req)
	writeJSON(w, http.StatusAccepted, campaigns.snapshot(job, false))
}

func (s *server) handleCampaignList(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	campaigns.mu.Lock()
	list := make([]*campaignJob, 0)
	for _, j := range campaigns.jobs {
		if j.SessionID == sess.id {
			list = append(list, j)
		}
	}
	campaigns.mu.Unlock()
	out := make([]map[string]any, 0, len(list))
	for _, j := range list {
		out = append(out, campaigns.snapshot(j, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": out})
}

func (s *server) handleCampaignGet(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	campaigns.mu.Lock()
	j := campaigns.jobs[r.PathValue("id")]
	campaigns.mu.Unlock()
	if j == nil || j.SessionID != sess.id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "campanha não encontrada"})
		return
	}
	writeJSON(w, http.StatusOK, campaigns.snapshot(j, true))
}

func (s *server) handleCampaignCancel(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	campaigns.mu.Lock()
	j := campaigns.jobs[r.PathValue("id")]
	campaigns.mu.Unlock()
	if j == nil || j.SessionID != sess.id {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "campanha não encontrada"})
		return
	}
	select {
	case <-j.cancel:
	default:
		close(j.cancel)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceling"})
}
