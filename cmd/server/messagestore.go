package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/encoding/protojson"
)

// storeMessageEvent persiste uma mensagem recebida (evento do whatsmeow) no
// histórico. Roda em background para não travar o loop de eventos.
func (s *Session) storeMessageEvent(evt *events.Message) {
	if s.mgr.store == nil {
		return
	}
	m := storedMessage{
		ChatJID:   evt.Info.Chat.String(),
		SenderJID: evt.Info.Sender.String(),
		MsgID:     evt.Info.ID,
		FromMe:    evt.Info.IsFromMe,
		Timestamp: evt.Info.Timestamp.UnixMilli(),
		Type:      messageType(evt.Message),
		Body:      messageText(evt.Message),
	}
	if raw, err := protojson.Marshal(evt.Message); err == nil {
		m.Raw = json.RawMessage(raw)
	}
	go func() { _ = s.mgr.store.saveMessage(s.mgr.appCtx, s.id, m) }()
}

// recordOutgoing persiste uma mensagem que ESTE cliente enviou. Só grava
// conteúdo de fato (texto/mídia/etc.); ações como reação, edição e revogação
// caem em type "unknown" e são ignoradas para não poluir o histórico.
func (s *Session) recordOutgoing(chat types.JID, msgID string, ts int64, msg *waE2E.Message) {
	// registra como "enviada por nós pela API" — o espelhamento no Chatwoot usa a
	// origem para decidir se reflete (toggle mirror_api) ou ignora
	s.markSelfSent(msgID, selfSentAPI)
	if s.mgr.store == nil {
		return
	}
	typ := messageType(msg)
	if typ == "unknown" {
		return
	}
	sender := chat
	if id := s.client.Store.ID; id != nil {
		sender = id.ToNonAD()
	}
	m := storedMessage{
		ChatJID:   chat.String(),
		SenderJID: sender.String(),
		MsgID:     msgID,
		FromMe:    true,
		Timestamp: ts,
		Type:      typ,
		Body:      messageText(msg),
	}
	if raw, err := protojson.Marshal(msg); err == nil {
		m.Raw = json.RawMessage(raw)
	}
	go func() { _ = s.mgr.store.saveMessage(s.mgr.appCtx, s.id, m) }()
}

// storedMessage é uma linha da tabela messages.
type storedMessage struct {
	ChatJID   string          `json:"chat"`
	SenderJID string          `json:"sender"`
	MsgID     string          `json:"id"`
	FromMe    bool            `json:"fromMe"`
	Timestamp int64           `json:"timestamp"`
	Type      string          `json:"type"`
	Body      string          `json:"body"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

type campaignSendRecord struct {
	ID            string `json:"id"`
	CampaignID    string `json:"campaignId"`
	CampaignName  string `json:"campaignName"`
	Recipient     string `json:"recipient"`
	MsgID         string `json:"msgId,omitempty"`
	Timestamp     int64  `json:"timestamp"`
	Kind          string `json:"kind"`
	Body          string `json:"body"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	ReceiptStatus string `json:"receiptStatus,omitempty"`
	ReceiptTS     int64  `json:"receiptTimestamp,omitempty"`
}

func receiptPriority(status string) int {
	switch status {
	case "played":
		return 4
	case "read":
		return 3
	case "delivered":
		return 2
	case "sent":
		return 1
	default:
		return 0
	}
}

func normalizeReceiptStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "played"):
		return "played"
	case strings.Contains(value, "read"):
		return "read"
	case strings.Contains(value, "deliver"):
		return "delivered"
	default:
		return ""
	}
}

// saveMessageReceipts persiste o melhor estado conhecido para cada mensagem.
// Recibos podem chegar fora de ordem, então um "delivered" tardio nunca reduz
// uma mensagem que já estava como "read".
func (s *sessionStore) saveMessageReceipts(ctx context.Context, sessionID, chatJID, status string, ts int64, messageIDs []types.MessageID) error {
	status = normalizeReceiptStatus(status)
	if status == "" || len(messageIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, messageID := range messageIDs {
		id := strings.TrimSpace(string(messageID))
		if id == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO message_receipts (session_id, msg_id, chat_jid, status, ts)
			VALUES ($1,$2,NULLIF($3,''),$4,$5)
			ON CONFLICT (session_id, msg_id) DO UPDATE SET
				chat_jid = COALESCE(EXCLUDED.chat_jid, message_receipts.chat_jid),
				status = CASE
					WHEN CASE EXCLUDED.status WHEN 'played' THEN 4 WHEN 'read' THEN 3 WHEN 'delivered' THEN 2 ELSE 1 END
						>= CASE message_receipts.status WHEN 'played' THEN 4 WHEN 'read' THEN 3 WHEN 'delivered' THEN 2 ELSE 1 END
					THEN EXCLUDED.status ELSE message_receipts.status END,
				ts = GREATEST(message_receipts.ts, EXCLUDED.ts)`,
			sessionID, id, chatJID, status, ts)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarshalJSON acrescenta aliases no estilo WAHA (from/chatId) sem remover os
// campos originais (id/body/timestamp/fromMe já coincidem com a WAHA).
func (m storedMessage) MarshalJSON() ([]byte, error) {
	type alias storedMessage
	return json.Marshal(struct {
		alias
		From   string `json:"from"`
		ChatID string `json:"chatId"`
	}{alias(m), waChatIDStr(m.SenderJID), waChatIDStr(m.ChatJID)})
}

// chatOverview resume uma conversa (última mensagem + contagem).
type chatOverview struct {
	ChatJID    string `json:"chat"`
	LastBody   string `json:"lastMessage"`
	LastType   string `json:"lastType"`
	LastTS     int64  `json:"timestamp"`
	Count      int    `json:"count"`
	LastFromMe bool   `json:"lastFromMe"`
}

// MarshalJSON acrescenta o alias id (chatId estilo WAHA).
func (o chatOverview) MarshalJSON() ([]byte, error) {
	type alias chatOverview
	return json.Marshal(struct {
		alias
		ID   string `json:"id"`
		Type string `json:"type"` // individual | group | channel | broadcast
	}{alias(o), waChatIDStr(o.ChatJID), chatKind(o.ChatJID)})
}

// saveMessage persiste (ou atualiza) uma mensagem. Idempotente por (session, chat, msg_id).
func (s *sessionStore) saveMessage(ctx context.Context, sessionID string, m storedMessage) error {
	var raw any
	if len(m.Raw) > 0 {
		raw = []byte(m.Raw)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO messages (session_id, chat_jid, sender_jid, msg_id, from_me, ts, type, body, raw)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (session_id, chat_jid, msg_id) DO UPDATE
			SET body = EXCLUDED.body, type = EXCLUDED.type, raw = EXCLUDED.raw`,
		sessionID, m.ChatJID, m.SenderJID, m.MsgID, m.FromMe, m.Timestamp, m.Type, m.Body, raw)
	return err
}

// findMessage acha uma mensagem pelo ID (usada p/ reconstruir a enquete e votar).
func (s *sessionStore) findMessage(ctx context.Context, sessionID, msgID string) (chatJID, senderJID string, fromMe bool, raw json.RawMessage, err error) {
	var body []byte
	err = s.db.QueryRowContext(ctx,
		`SELECT chat_jid, sender_jid, from_me, raw FROM messages WHERE session_id = $1 AND msg_id = $2 LIMIT 1`,
		sessionID, msgID).Scan(&chatJID, &senderJID, &fromMe, &body)
	if err == nil && len(body) > 0 {
		raw = json.RawMessage(body)
	}
	return
}

// listMessages devolve as mensagens de um chat, mais recentes primeiro.
func (s *sessionStore) listMessages(ctx context.Context, sessionID, chatJID string, limit, offset int, withRaw bool) ([]storedMessage, error) {
	rawCol := "NULL"
	if withRaw {
		rawCol = "raw"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_jid, sender_jid, msg_id, from_me, ts, type, COALESCE(body, ''), `+rawCol+`
		FROM messages WHERE session_id = $1 AND chat_jid = $2
		ORDER BY ts DESC LIMIT $3 OFFSET $4`, sessionID, chatJID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// listInteractiveResponses recupera respostas já persistidas. Inclui os cliques
// reconhecidos e também texto digitado: algumas versões do WhatsApp convertem a
// seleção da lista em texto simples, portanto filtrar só pelo envelope perderia
// respostas reais de campanhas antigas.
func (s *sessionStore) listInteractiveResponses(ctx context.Context, sessionID string, limit int) ([]storedMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.chat_jid, m.sender_jid, m.msg_id, m.from_me, m.ts, m.type, COALESCE(m.body, ''), NULL
		FROM messages m
		WHERE m.session_id = $1 AND m.from_me = false AND COALESCE(m.body, '') <> ''
		AND (m.chat_jid LIKE '%@s.whatsapp.net' OR m.chat_jid LIKE '%@lid') AND (
			m.raw ? 'interactiveResponseMessage' OR m.raw ? 'listResponseMessage' OR
			m.raw ? 'buttonsResponseMessage' OR m.raw ? 'templateButtonReplyMessage' OR
			EXISTS (
				SELECT 1 FROM messages sent
				WHERE sent.session_id = m.session_id AND sent.chat_jid = m.chat_jid
				AND sent.from_me = true AND sent.ts <= m.ts AND m.ts - sent.ts <= 604800
				AND (sent.raw ? 'listMessage' OR sent.raw ? 'interactiveMessage' OR
					sent.raw ? 'buttonsMessage' OR sent.raw ? 'templateMessage')
			)
		)
		ORDER BY m.ts DESC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (s *sessionStore) saveCampaignSend(ctx context.Context, sessionID string, record campaignSendRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO campaign_sends (session_id, campaign_id, campaign_name, recipient, msg_id, ts, kind, body, status, last_error)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,NULLIF($10,''))
		ON CONFLICT (session_id, msg_id) WHERE msg_id IS NOT NULL AND msg_id <> '' DO NOTHING`,
		sessionID, record.CampaignID, record.CampaignName, record.Recipient, record.MsgID,
		record.Timestamp, record.Kind, record.Body, record.Status, record.Error)
	return err
}

// listCampaignSends combina a auditoria persistente das campanhas novas com
// listas/botões antigos recuperados do histórico de mensagens.
func (s *sessionStore) listCampaignSends(ctx context.Context, sessionID string, limit int) ([]campaignSendRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH sent (id, campaign_id, campaign_name, recipient, msg_id, ts, kind, body, status, last_error) AS (
			SELECT id::text, campaign_id, campaign_name, recipient, COALESCE(msg_id, ''), ts,
				kind, COALESCE(body, ''), status, COALESCE(last_error, '')
			FROM campaign_sends WHERE session_id = $1
			UNION ALL
			SELECT 'legacy:' || m.msg_id, '', 'Disparo recuperado', m.chat_jid, m.msg_id, m.ts,
				CASE WHEN m.raw ? 'listMessage' THEN 'list'
					WHEN m.raw ? 'interactiveMessage' THEN 'interactive'
					WHEN m.raw ? 'buttonsMessage' OR m.raw #> '{documentWithCaptionMessage,message,buttonsMessage}' IS NOT NULL THEN 'buttons'
					ELSE 'message' END,
				COALESCE(m.body, ''), 'sent', ''
			FROM messages m
			WHERE m.session_id = $1 AND m.from_me = true
			AND (m.chat_jid LIKE '%@s.whatsapp.net' OR m.chat_jid LIKE '%@lid')
			AND (m.raw ? 'listMessage' OR m.raw ? 'interactiveMessage' OR m.raw ? 'buttonsMessage' OR
				m.raw #> '{documentWithCaptionMessage,message,buttonsMessage}' IS NOT NULL)
			AND NOT EXISTS (SELECT 1 FROM campaign_sends cs WHERE cs.session_id = m.session_id AND cs.msg_id = m.msg_id)
		)
		SELECT sent.*, COALESCE(mr.status, ''), COALESCE(mr.ts, 0)
		FROM sent
		LEFT JOIN message_receipts mr ON mr.session_id = $1 AND mr.msg_id = sent.msg_id
		ORDER BY sent.ts DESC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []campaignSendRecord{}
	for rows.Next() {
		var record campaignSendRecord
		if err := rows.Scan(&record.ID, &record.CampaignID, &record.CampaignName, &record.Recipient,
			&record.MsgID, &record.Timestamp, &record.Kind, &record.Body, &record.Status, &record.Error,
			&record.ReceiptStatus, &record.ReceiptTS); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	return s.recoverCampaignTextEchoes(ctx, sessionID, out, limit)
}

// recoverCampaignTextEchoes cobre clientes que espelham no histórico uma lista
// enviada como texto comum. Usamos os corpos das listas confirmadas como sementes
// e só aceitamos mensagens muito semelhantes dentro da mesma janela de envio.
func (s *sessionStore) recoverCampaignTextEchoes(ctx context.Context, sessionID string, records []campaignSendRecord, limit int) ([]campaignSendRecord, error) {
	if len(records) == 0 {
		return records, nil
	}
	seen := make(map[string]struct{}, len(records))
	seeds := make([]map[string]struct{}, 0, len(records))
	minTS, maxTS := records[0].Timestamp, records[0].Timestamp
	for _, record := range records {
		seen[record.MsgID] = struct{}{}
		if record.Timestamp < minTS {
			minTS = record.Timestamp
		}
		if record.Timestamp > maxTS {
			maxTS = record.Timestamp
		}
		if tokens := campaignTokens(record.Body); len(tokens) >= 12 {
			seeds = append(seeds, tokens)
		}
	}
	if len(seeds) == 0 {
		return records, nil
	}
	const window = int64(6 * 60 * 60 * 1000)
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_jid, msg_id, ts, COALESCE(body, '')
		FROM messages
		WHERE session_id = $1 AND from_me = true AND COALESCE(body, '') <> ''
		AND (chat_jid LIKE '%@s.whatsapp.net' OR chat_jid LIKE '%@lid')
		AND ts BETWEEN $2 AND $3
		ORDER BY ts DESC LIMIT 5000`, sessionID, minTS-window, maxTS+window)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var recipient, msgID, body string
		var ts int64
		if err := rows.Scan(&recipient, &msgID, &ts, &body); err != nil {
			return nil, err
		}
		if _, ok := seen[msgID]; ok {
			continue
		}
		tokens := campaignTokens(body)
		matched := false
		for _, seed := range seeds {
			if campaignSimilarity(tokens, seed) >= 0.55 {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		seen[msgID] = struct{}{}
		records = append(records, campaignSendRecord{
			ID: "legacy-text:" + msgID, CampaignName: "Disparo recuperado",
			Recipient: recipient, MsgID: msgID, Timestamp: ts, Kind: "text",
			Body: body, Status: "sent",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Timestamp > records[j].Timestamp })
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func campaignTokens(value string) map[string]struct{} {
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make(map[string]struct{}, len(words))
	for _, word := range words {
		if len([]rune(word)) >= 3 {
			out[word] = struct{}{}
		}
	}
	return out
}

func campaignSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if _, ok := b[token]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}

// searchMessages busca por texto no corpo das mensagens (ILIKE, case-insensitive).
// Se chatJID != "", limita ao chat. Mais recentes primeiro.
func (s *sessionStore) searchMessages(ctx context.Context, sessionID, query, chatJID string, limit int) ([]storedMessage, error) {
	like := "%" + query + "%"
	var rows *sql.Rows
	var err error
	if chatJID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT chat_jid, sender_jid, msg_id, from_me, ts, type, COALESCE(body, ''), NULL
			FROM messages WHERE session_id = $1 AND chat_jid = $2 AND body ILIKE $3
			ORDER BY ts DESC LIMIT $4`, sessionID, chatJID, like, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT chat_jid, sender_jid, msg_id, from_me, ts, type, COALESCE(body, ''), NULL
			FROM messages WHERE session_id = $1 AND body ILIKE $2
			ORDER BY ts DESC LIMIT $3`, sessionID, like, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]storedMessage, error) {
	out := []storedMessage{}
	for rows.Next() {
		var m storedMessage
		var raw []byte
		if err := rows.Scan(&m.ChatJID, &m.SenderJID, &m.MsgID, &m.FromMe, &m.Timestamp, &m.Type, &m.Body, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			m.Raw = json.RawMessage(raw)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// anchorMessage é a mensagem mais antiga conhecida de um chat 1:1 — usada como
// âncora no pedido de HistorySync SOB DEMANDA (pede mensagens mais antigas que ela).
type anchorMessage struct {
	ChatJID string
	MsgID   string
	TS      int64 // epoch ms
	FromMe  bool
}

// oldestPerChat devolve a msg mais antiga de cada chat 1:1 (@s.whatsapp.net/@lid),
// mais recentes primeiro, até limitChats chats. Ignora grupos e canais.
func (s *sessionStore) oldestPerChat(ctx context.Context, sessionID string, limitChats int) ([]anchorMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chat_jid, msg_id, ts, from_me FROM (
			SELECT DISTINCT ON (chat_jid) chat_jid, msg_id, ts, from_me
			FROM messages
			WHERE session_id = $1
			  AND chat_jid NOT LIKE '%@g.us'
			  AND chat_jid NOT LIKE '%@newsletter'
			  AND chat_jid NOT LIKE '%@broadcast'
			ORDER BY chat_jid, ts ASC
		) t
		ORDER BY ts DESC LIMIT $2`, sessionID, limitChats)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []anchorMessage{}
	for rows.Next() {
		var a anchorMessage
		if err := rows.Scan(&a.ChatJID, &a.MsgID, &a.TS, &a.FromMe); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// oldestInChat devolve a msg mais antiga de um chat específico (âncora on-demand).
func (s *sessionStore) oldestInChat(ctx context.Context, sessionID, chatJID string) (anchorMessage, error) {
	var a anchorMessage
	err := s.db.QueryRowContext(ctx, `
		SELECT chat_jid, msg_id, ts, from_me FROM messages
		WHERE session_id = $1 AND chat_jid = $2
		ORDER BY ts ASC LIMIT 1`, sessionID, chatJID).Scan(&a.ChatJID, &a.MsgID, &a.TS, &a.FromMe)
	if err == sql.ErrNoRows {
		return a, nil
	}
	return a, err
}

// listChats devolve uma visão geral das conversas (uma linha por chat_jid),
// ordenadas pela última mensagem.
func (s *sessionStore) listChats(ctx context.Context, sessionID string, limit, offset int) ([]chatOverview, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.chat_jid, m.body, m.type, m.ts, m.from_me, c.cnt
		FROM messages m
		JOIN (
			SELECT chat_jid, MAX(ts) AS max_ts, COUNT(*) AS cnt
			FROM messages WHERE session_id = $1 GROUP BY chat_jid
		) c ON c.chat_jid = m.chat_jid AND c.max_ts = m.ts
		WHERE m.session_id = $1
		ORDER BY m.ts DESC LIMIT $2 OFFSET $3`, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []chatOverview{}
	for rows.Next() {
		var o chatOverview
		var body sql.NullString
		if err := rows.Scan(&o.ChatJID, &body, &o.LastType, &o.LastTS, &o.LastFromMe, &o.Count); err != nil {
			return nil, err
		}
		o.LastBody = body.String
		out = append(out, o)
	}
	return out, rows.Err()
}
