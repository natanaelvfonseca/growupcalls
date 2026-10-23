package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// queryPage lê limit/offset da query string com defaults sensatos.
func queryPage(r *http.Request, defLimit int) (limit, offset int) {
	limit, offset = defLimit, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

// GET /api/sessions/{sid}/chats?limit=&offset=  → visão geral das conversas
func (s *server) handleListChats(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	limit, offset := queryPage(r, 100)
	chats, err := s.sessions.store.listChats(r.Context(), sess.id, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, chats)
}

// GET /api/sessions/{sid}/chats/{chatId}/messages?limit=&offset=&raw=
func (s *server) handleChatMessages(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	chat, err := resolveRecipient(r.PathValue("chatId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit, offset := queryPage(r, 50)
	msgs, err := s.sessions.store.listMessages(r.Context(), sess.id, chat.String(), limit, offset, r.URL.Query().Get("raw") == "true")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GET /api/sessions/{sid}/messages/search?q=&chatId=&limit=
// Busca full-text (ILIKE) no corpo das mensagens do histórico.
func (s *server) handleSearchMessages(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q required"})
		return
	}
	chatJID := ""
	if c := strings.TrimSpace(r.URL.Query().Get("chatId")); c != "" {
		chat, err := resolveRecipient(c)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		chatJID = chat.String()
	}
	limit, _ := queryPage(r, 50)
	msgs, err := s.sessions.store.searchMessages(r.Context(), sess.id, q, chatJID, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GET /api/sessions/{sid}/messages?chatId=&limit=&offset=&raw=
// Atalho para as mensagens de um chat via query string.
func (s *server) handleQueryMessages(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	chatID := r.URL.Query().Get("chatId")
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chatId required"})
		return
	}
	chat, err := resolveRecipient(chatID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	limit, offset := queryPage(r, 50)
	msgs, err := s.sessions.store.listMessages(r.Context(), sess.id, chat.String(), limit, offset, r.URL.Query().Get("raw") == "true")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// GET /api/sessions/{sid}/campaign-responses?limit=200
// Respostas já recebidas de listas e botões, inclusive native flow.
func (s *server) handleCampaignResponses(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	limit, _ := queryPage(r, 200)
	responses, err := s.sessions.store.listInteractiveResponses(r.Context(), sess.id, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Converte @lid em telefone quando o device store conhece o pareamento.
	for i := range responses {
		responses[i].ChatJID = sess.callPeerOut(responses[i].ChatJID)
		responses[i].SenderJID = sess.callPeerOut(responses[i].SenderJID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"responses": responses})
}

// GET /api/sessions/{sid}/campaign-sends?limit=500
// Destinatários e mensagens enviados por campanhas, persistentes entre deploys.
func (s *server) handleCampaignSends(w http.ResponseWriter, r *http.Request) {
	sess := s.sessionByID(w, r.PathValue("sid"))
	if sess == nil {
		return
	}
	limit, _ := queryPage(r, 500)
	records, err := s.sessions.store.listCampaignSends(r.Context(), sess.id, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for i := range records {
		// Campanhas novas guardam o telefone puro; históricos antigos guardam
		// JID. callPeerOut só aceita JID e devolvia vazio para os telefones novos.
		if strings.Contains(records[i].Recipient, "@") {
			if phone := sess.callPeerOut(records[i].Recipient); phone != "" {
				records[i].Recipient = phone
			}
		} else {
			records[i].Recipient = normalizePhone(records[i].Recipient)
		}
	}
	// Se o formato antigo não preservou o envio, uma resposta interativa ainda é
	// evidência suficiente de que aquele contato recebeu a campanha.
	knownRecipients := make(map[string]struct{}, len(records))
	for _, record := range records {
		knownRecipients[record.Recipient] = struct{}{}
	}
	responses, responseErr := s.sessions.store.listInteractiveResponses(r.Context(), sess.id, limit)
	if responseErr == nil {
		for _, response := range responses {
			recipient := sess.callPeerOut(response.ChatJID)
			if _, exists := knownRecipients[recipient]; exists {
				continue
			}
			knownRecipients[recipient] = struct{}{}
			records = append(records, campaignSendRecord{
				ID: "response-proof:" + response.MsgID, CampaignName: "Confirmado pela resposta",
				Recipient: recipient, Timestamp: response.Timestamp, Kind: "message",
				Body: "Resposta recebida: " + response.Body, Status: "sent",
			})
		}
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Timestamp > records[j].Timestamp })
	writeJSON(w, http.StatusOK, map[string]any{"sends": records})
}
