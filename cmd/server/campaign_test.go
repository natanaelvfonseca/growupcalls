package main

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestRenderCampaignContent(t *testing.T) {
	src := CampaignContent{
		Kind:       "list",
		Body:       "Olá, {{nome}}. Parcela: {{valor}}",
		Variants:   []string{"Oi, {{nome}}", "Olá, {{nome}} — {{valor}}"},
		ButtonText: "Opções de {{nome}}",
		Sections: []CampaignSection{{
			Title: "Contrato {{contrato}}",
			Rows:  []CampaignRow{{ID: "pagar_{{contrato}}", Title: "Pagar {{valor}}"}},
		}},
	}

	got := renderCampaignContent(src, map[string]string{
		"nome": "Ana", "valor": "R$ 100", "contrato": "42",
	})

	if got.Body != "Olá, Ana. Parcela: R$ 100" {
		t.Fatalf("body não personalizado: %q", got.Body)
	}
	if len(got.Variants) != 2 || got.Variants[0] != "Oi, Ana" || got.Variants[1] != "Olá, Ana — R$ 100" {
		t.Fatalf("variações não personalizadas: %#v", got.Variants)
	}
	if got.ButtonText != "Opções de Ana" || got.Sections[0].Title != "Contrato 42" {
		t.Fatalf("campos da lista não personalizados: %#v", got)
	}
	if got.Sections[0].Rows[0].ID != "pagar_42" || got.Sections[0].Rows[0].Title != "Pagar R$ 100" {
		t.Fatalf("linha da lista não personalizada: %#v", got.Sections[0].Rows[0])
	}
	if src.Body != "Olá, {{nome}}. Parcela: {{valor}}" {
		t.Fatal("render alterou o template original")
	}
}

func TestBuildCampaignText(t *testing.T) {
	msg, interactive, err := buildCampaignMessage(nil, typesJIDForTest(), CampaignContent{Kind: "text", Body: "Olá"})
	if err != nil || interactive || msg.GetConversation() != "Olá" {
		t.Fatalf("mensagem de texto inválida: msg=%v interactive=%v err=%v", msg, interactive, err)
	}
}

func TestBuildCampaignClassicList(t *testing.T) {
	msg, interactive, err := buildCampaignMessage(nil, typesJIDForTest(), CampaignContent{
		Kind: "list", Body: "Escolha", ButtonText: "Abrir", ListMode: "classic",
		Sections: []CampaignSection{{Title: "Opções", Rows: []CampaignRow{{ID: "sim", Title: "Sim"}}}},
	})
	if err != nil || interactive || msg.GetListMessage() == nil {
		t.Fatalf("lista clássica inválida: interactive=%v err=%v msg=%v", interactive, err, msg)
	}
	if got := msg.GetListMessage().GetSections()[0].GetRows()[0].GetRowID(); got != "sim" {
		t.Fatalf("row id = %q", got)
	}
}

func TestNormalizeReceiptStatus(t *testing.T) {
	cases := map[string]string{
		"delivered": "delivered",
		"read":      "read",
		"read-self": "read",
		"played":    "played",
		"sender":    "",
	}
	for input, want := range cases {
		if got := normalizeReceiptStatus(input); got != want {
			t.Fatalf("normalizeReceiptStatus(%q) = %q, want %q", input, got, want)
		}
	}
	if receiptPriority("delivered") >= receiptPriority("read") || receiptPriority("read") >= receiptPriority("played") {
		t.Fatal("prioridade dos recibos está fora de ordem")
	}
}

// O tipo é produzido pelo mesmo parser usado pelos endpoints, evitando fixtures
// de JID divergentes da versão do whatsmeow pinada pelo projeto.
func typesJIDForTest() types.JID {
	jid, _ := types.ParseJID("5511999999999@s.whatsapp.net")
	return jid
}
