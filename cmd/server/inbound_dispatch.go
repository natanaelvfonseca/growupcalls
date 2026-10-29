package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const jovemAprendizSource = "jovem-aprendiz"

var templateIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

type messageTemplate struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Content   CampaignContent `json:"content"`
	DelayMs   int             `json:"delayMs"`
	JitterMs  int             `json:"jitterMs"`
	Active    bool            `json:"active"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

type dispatchJobRef struct {
	SessionID  string `json:"sessionId"`
	CampaignID string `json:"campaignId"`
	Recipients int    `json:"recipients"`
}

type integrationDispatch struct {
	Source         string           `json:"source"`
	EventID        string           `json:"eventId"`
	TemplateID     string           `json:"templateId"`
	Status         string           `json:"status"`
	RecipientCount int              `json:"recipientCount"`
	Jobs           []dispatchJobRef `json:"jobs"`
	Error          string           `json:"error,omitempty"`
	CreatedAt      int64            `json:"createdAt"`
	UpdatedAt      int64            `json:"updatedAt"`
}

type inboundDispatchRequest struct {
	Type                  string            `json:"type"`
	EventIDCamel          string            `json:"eventId"`
	EventIDSnake          string            `json:"event_id"`
	ID                    string            `json:"id"`
	TemplateCamel         string            `json:"templateId"`
	TemplateSnake         string            `json:"template_id"`
	Phones                []string          `json:"phones"`
	Phone                 string            `json:"phone"`
	Phone2                string            `json:"phone_2"`
	Telefone              string            `json:"telefone"`
	Telefone2             string            `json:"telefone_2"`
	Variables             map[string]string `json:"variables"`
	Data                  map[string]any    `json:"data"`
	Student               map[string]any    `json:"student"`
	Aluno                 map[string]any    `json:"aluno"`
	SenderSessionIDs      []string          `json:"senderSessionIds"`
	SenderSessionIDsSnake []string          `json:"sender_session_ids"`
}

func (r inboundDispatchRequest) eventID() string {
	for _, value := range []string{r.EventIDCamel, r.EventIDSnake, r.ID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (r inboundDispatchRequest) templateID() string {
	if value := strings.TrimSpace(r.TemplateCamel); value != "" {
		return value
	}
	if value := strings.TrimSpace(r.TemplateSnake); value != "" {
		return value
	}
	for _, key := range []string{"template_id", "templateId"} {
		if value := scalarString(r.Data[key]); value != "" {
			return value
		}
	}
	if strings.EqualFold(strings.TrimSpace(r.Type), "exam.completed") {
		return "prova-concluida"
	}
	return ""
}

func scalarString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

func (r inboundDispatchRequest) normalized() ([]string, map[string]string, []string, error) {
	variables := make(map[string]string, len(r.Variables)+12)
	for key, value := range r.Variables {
		variables[strings.TrimSpace(key)] = value
	}
	phones := append([]string(nil), r.Phones...)
	phones = append(phones, r.Phone, r.Phone2, r.Telefone, r.Telefone2)
	for _, bag := range []map[string]any{r.Data, r.Student, r.Aluno} {
		for key, raw := range bag {
			if strings.EqualFold(strings.TrimSpace(key), "guardian") || strings.EqualFold(strings.TrimSpace(key), "responsavel") {
				if guardian, ok := raw.(map[string]any); ok {
					if name := scalarString(guardian["name"]); name != "" {
						variables["responsavel"] = name
						variables["guardian_name"] = name
					}
					phone := scalarString(guardian["phone"])
					if phone == "" {
						phone = scalarString(guardian["telefone"])
					}
					if phone != "" {
						phones = append(phones, phone)
						variables["telefone_responsavel"] = phone
						variables["guardian_phone"] = phone
					}
				}
				continue
			}
			value := scalarString(raw)
			if value == "" {
				continue
			}
			variables[key] = value
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "name", "nome", "full_name":
				variables["nome"] = value
			case "score_percent", "percentual", "nota":
				variables["nota"] = value
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "phone", "phone_1", "phone1", "telefone", "telefone_1", "telefone1", "celular", "whatsapp":
				phones = append(phones, value)
			case "phone_2", "phone2", "telefone_2", "telefone2", "celular_2", "whatsapp_2":
				phones = append(phones, value)
			}
		}
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(phones))
	for _, raw := range phones {
		phone := normalizePhone(raw)
		if len(phone) < 10 || len(phone) > 15 {
			continue
		}
		if _, ok := seen[phone]; ok {
			continue
		}
		seen[phone] = struct{}{}
		normalized = append(normalized, phone)
	}
	if len(normalized) == 0 {
		return nil, nil, nil, errors.New("nenhum telefone válido no webhook")
	}
	allowed := append([]string(nil), r.SenderSessionIDs...)
	allowed = append(allowed, r.SenderSessionIDsSnake...)
	return normalized, variables, allowed, nil
}

func validateMessageTemplate(t messageTemplate) error {
	if !templateIDPattern.MatchString(t.ID) {
		return errors.New("id do modelo inválido")
	}
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("nome do modelo obrigatório")
	}
	switch t.Content.Kind {
	case "text", "buttons", "interactive", "list":
	default:
		return errors.New("tipo do modelo inválido")
	}
	if strings.TrimSpace(t.Content.Body) == "" && len(t.Content.Variants) == 0 {
		return errors.New("o modelo precisa de body ou variants")
	}
	if t.DelayMs < 1000 {
		t.DelayMs = 3000
	}
	if t.JitterMs < 0 || t.JitterMs > 60000 {
		return errors.New("jitterMs deve ficar entre 0 e 60000")
	}
	return nil
}

func (s *sessionStore) putMessageTemplate(ctx context.Context, t messageTemplate) (messageTemplate, error) {
	now := time.Now().UnixMilli()
	if t.DelayMs < 1000 {
		t.DelayMs = 3000
	}
	body, err := json.Marshal(t.Content)
	if err != nil {
		return t, err
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO message_templates (id,name,content,delay_ms,jitter_ms,active,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name,content=EXCLUDED.content,delay_ms=EXCLUDED.delay_ms,jitter_ms=EXCLUDED.jitter_ms,active=EXCLUDED.active,updated_at=EXCLUDED.updated_at
		RETURNING created_at,updated_at`, t.ID, t.Name, body, t.DelayMs, t.JitterMs, t.Active, now).Scan(&t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func scanMessageTemplate(row interface{ Scan(...any) error }) (messageTemplate, error) {
	var t messageTemplate
	var content []byte
	err := row.Scan(&t.ID, &t.Name, &content, &t.DelayMs, &t.JitterMs, &t.Active, &t.CreatedAt, &t.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(content, &t.Content)
	}
	return t, err
}

func (s *sessionStore) getMessageTemplate(ctx context.Context, id string) (messageTemplate, error) {
	return scanMessageTemplate(s.db.QueryRowContext(ctx, `SELECT id,name,content,delay_ms,jitter_ms,active,created_at,updated_at FROM message_templates WHERE id=$1`, id))
}

func (s *sessionStore) listMessageTemplates(ctx context.Context) ([]messageTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,content,delay_ms,jitter_ms,active,created_at,updated_at FROM message_templates ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []messageTemplate{}
	for rows.Next() {
		t, scanErr := scanMessageTemplate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sessionStore) claimDispatch(ctx context.Context, source, eventID, templateID string) (integrationDispatch, bool, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO integration_dispatches (source,event_id,template_id,status,created_at,updated_at)
		VALUES ($1,$2,$3,'processing',$4,$4)
		ON CONFLICT (source,event_id) DO UPDATE
		SET template_id=EXCLUDED.template_id,status='processing',last_error=NULL,updated_at=EXCLUDED.updated_at
		WHERE integration_dispatches.status='failed'`, source, eventID, templateID, now)
	if err != nil {
		return integrationDispatch{}, false, err
	}
	count, _ := result.RowsAffected()
	d, err := s.getDispatch(ctx, source, eventID)
	return d, count == 1, err
}

func (s *sessionStore) getDispatch(ctx context.Context, source, eventID string) (integrationDispatch, error) {
	var d integrationDispatch
	var jobs []byte
	err := s.db.QueryRowContext(ctx, `SELECT source,event_id,template_id,status,recipient_count,jobs,COALESCE(last_error,''),created_at,updated_at FROM integration_dispatches WHERE source=$1 AND event_id=$2`, source, eventID).Scan(&d.Source, &d.EventID, &d.TemplateID, &d.Status, &d.RecipientCount, &jobs, &d.Error, &d.CreatedAt, &d.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(jobs, &d.Jobs)
	}
	return d, err
}

func (s *sessionStore) listDispatches(ctx context.Context, source string, limit int) ([]integrationDispatch, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source,event_id,template_id,status,recipient_count,jobs,COALESCE(last_error,''),created_at,updated_at FROM integration_dispatches WHERE source=$1 ORDER BY created_at DESC LIMIT $2`, source, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []integrationDispatch{}
	for rows.Next() {
		var d integrationDispatch
		var jobs []byte
		if err := rows.Scan(&d.Source, &d.EventID, &d.TemplateID, &d.Status, &d.RecipientCount, &jobs, &d.Error, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jobs, &d.Jobs); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (s *sessionStore) finishDispatch(ctx context.Context, d integrationDispatch) error {
	jobs, _ := json.Marshal(d.Jobs)
	d.UpdatedAt = time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE integration_dispatches SET status=$1,recipient_count=$2,jobs=$3,last_error=NULLIF($4,''),updated_at=$5 WHERE source=$6 AND event_id=$7`, d.Status, d.RecipientCount, jobs, d.Error, d.UpdatedAt, d.Source, d.EventID)
	return err
}

func (s *sessionStore) reserveSenderPositions(ctx context.Context, source string, count int) (int64, error) {
	var start int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO dispatch_cursors (source,next_position) VALUES ($1,$2) ON CONFLICT (source) DO UPDATE SET next_position=dispatch_cursors.next_position+EXCLUDED.next_position RETURNING next_position-$2`, source, count).Scan(&start)
	return start, err
}

func (m *SessionManager) dispatchSessions(allowed []string) []*Session {
	allow := map[string]struct{}{}
	for _, id := range allowed {
		if id = strings.TrimSpace(id); id != "" {
			allow[id] = struct{}{}
		}
	}
	m.mu.RLock()
	ordered := make([]*Session, 0, len(m.order))
	for _, id := range m.order {
		sess := m.sessions[id]
		if sess == nil || sess.client.Store.ID == nil || !sess.client.IsConnected() {
			continue
		}
		if len(allow) > 0 {
			if _, ok := allow[id]; !ok {
				continue
			}
		}
		ordered = append(ordered, sess)
	}
	m.mu.RUnlock()
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].id < ordered[j].id })
	return ordered
}

func (s *server) handleMessageTemplatePut(w http.ResponseWriter, r *http.Request) {
	// Um modelo recém-criado nasce ativo; enviar active=false continua sendo
	// respeitado para pausar um modelo existente.
	t := messageTemplate{Active: true}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if decoder.Decode(&t) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "modelo inválido"})
		return
	}
	t.ID = strings.TrimSpace(r.PathValue("id"))
	t.Name = strings.TrimSpace(t.Name)
	if err := validateMessageTemplate(t); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	stored, err := s.sessions.store.putMessageTemplate(r.Context(), t)
	if err != nil {
		s.log.Error("save message template failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "não foi possível salvar o modelo"})
		return
	}
	writeJSON(w, http.StatusOK, stored)
}

func (s *server) handleJovemAprendizDispatchGet(w http.ResponseWriter, r *http.Request) {
	d, err := s.sessions.store.getDispatch(r.Context(), jovemAprendizSource, r.PathValue("eventId"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "evento não encontrado"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "não foi possível carregar o evento"})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) handleJovemAprendizDispatchList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.sessions.store.listDispatches(r.Context(), jovemAprendizSource, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "não foi possível carregar os eventos"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dispatches": items})
}

func (s *server) handleMessageTemplateGet(w http.ResponseWriter, r *http.Request) {
	t, err := s.sessions.store.getMessageTemplate(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "modelo não encontrado"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "não foi possível carregar o modelo"})
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *server) handleMessageTemplateList(w http.ResponseWriter, r *http.Request) {
	items, err := s.sessions.store.listMessageTemplates(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "não foi possível carregar os modelos"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": items})
}

func (s *server) handleJovemAprendizWebhook(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.UseNumber()
	var req inboundDispatchRequest
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload inválido"})
		return
	}
	eventID, templateID := req.eventID(), req.templateID()
	if eventID == "" || len(eventID) > 160 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "event_id obrigatório"})
		return
	}
	if !templateIDPattern.MatchString(templateID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id inválido"})
		return
	}
	phones, variables, allowed, err := req.normalized()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	template, err := s.sessions.store.getMessageTemplate(r.Context(), templateID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "modelo não encontrado"})
		return
	}
	if err != nil || !template.Active {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "modelo indisponível"})
		return
	}
	d, claimed, err := s.sessions.store.claimDispatch(r.Context(), jovemAprendizSource, eventID, templateID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "falha ao registrar webhook"})
		return
	}
	if !claimed {
		writeJSON(w, http.StatusOK, map[string]any{"duplicate": true, "dispatch": d})
		return
	}
	sessions := s.sessions.dispatchSessions(allowed)
	if len(sessions) == 0 {
		d.Status = "failed"
		d.Error = "nenhum remetente conectado"
		d.RecipientCount = len(phones)
		_ = s.sessions.store.finishDispatch(r.Context(), d)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": d.Error, "dispatch": d})
		return
	}
	start, err := s.sessions.store.reserveSenderPositions(r.Context(), jovemAprendizSource, len(phones))
	if err != nil {
		d.Status = "failed"
		d.Error = "falha ao reservar remetentes"
		_ = s.sessions.store.finishDispatch(r.Context(), d)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": d.Error})
		return
	}
	type group struct {
		sess       *Session
		recipients []CampaignRecipient
	}
	groups := map[string]*group{}
	order := []string{}
	for i, phone := range phones {
		sess := sessions[(int(start)+i)%len(sessions)]
		g := groups[sess.id]
		if g == nil {
			g = &group{sess: sess}
			groups[sess.id] = g
			order = append(order, sess.id)
		}
		vars := make(map[string]string, len(variables)+1)
		for k, v := range variables {
			vars[k] = v
		}
		body := template.Content.Body
		if len(template.Content.Variants) > 0 {
			body = template.Content.Variants[(int(start)+i)%len(template.Content.Variants)]
		}
		vars["__growup_body"] = renderCampaignTemplate(body, vars)
		g.recipients = append(g.recipients, CampaignRecipient{Phone: phone, Variables: vars})
	}
	content := template.Content
	content.Body = "{{__growup_body}}"
	content.Variants = nil
	for _, sid := range order {
		g := groups[sid]
		job := campaigns.start(g.sess, campaignRequest{Name: fmt.Sprintf("%s · %s", template.Name, eventID), Recipients: g.recipients, Content: content, DelayMs: template.DelayMs, JitterMs: template.JitterMs})
		d.Jobs = append(d.Jobs, dispatchJobRef{SessionID: sid, CampaignID: job.ID, Recipients: len(g.recipients)})
	}
	d.Status = "accepted"
	d.RecipientCount = len(phones)
	if err := s.sessions.store.finishDispatch(r.Context(), d); err != nil {
		s.log.Error("finish inbound dispatch failed", "source", jovemAprendizSource, "eventId", eventID, "err", err)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"duplicate": false, "dispatch": d})
}
