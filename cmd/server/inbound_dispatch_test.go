package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInboundDispatchNormalizesTwoPhonesAndVariables(t *testing.T) {
	req := inboundDispatchRequest{
		Telefone:  "+55 (47) 99999-1111",
		Aluno:     map[string]any{"nome": "Ana", "telefone_2": json.Number("5547999992222")},
		Variables: map[string]string{"curso": "Aprendizagem"},
	}
	phones, variables, _, err := req.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if len(phones) != 2 || phones[0] != "5547999991111" || phones[1] != "5547999992222" {
		t.Fatalf("telefones inesperados: %#v", phones)
	}
	if variables["nome"] != "Ana" || variables["curso"] != "Aprendizagem" {
		t.Fatalf("variáveis inesperadas: %#v", variables)
	}
}

func TestInboundDispatchDeduplicatesPhones(t *testing.T) {
	req := inboundDispatchRequest{Phones: []string{"+55 47 99999-1111", "5547999991111"}}
	phones, _, _, err := req.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if len(phones) != 1 {
		t.Fatalf("esperava 1 telefone, recebeu %#v", phones)
	}
}

func TestMessageTemplateValidation(t *testing.T) {
	valid := messageTemplate{ID: "cobranca-1", Name: "Cobrança", Content: CampaignContent{Kind: "text", Variants: []string{"Olá {{nome}}"}}}
	if err := validateMessageTemplate(valid); err != nil {
		t.Fatalf("modelo válido rejeitado: %v", err)
	}
	valid.Content.Kind = "unknown"
	if err := validateMessageTemplate(valid); err == nil {
		t.Fatal("tipo inválido foi aceito")
	}
}

func TestJovemAprendizPayloadMapsStudentAndGuardian(t *testing.T) {
	req := inboundDispatchRequest{
		Type: "exam.completed",
		Student: map[string]any{
			"name":  "Aluno Teste",
			"phone": "5547999991111",
			"guardian": map[string]any{
				"name":  "Responsável Teste",
				"phone": "5547999992222",
			},
		},
		Data: map[string]any{"score_percent": json.Number("82.5")},
	}
	if got := req.templateID(); got != "prova-concluida" {
		t.Fatalf("modelo inesperado: %s", got)
	}
	phones, variables, _, err := req.normalized()
	if err != nil {
		t.Fatal(err)
	}
	if len(phones) != 2 || phones[0] != "5547999991111" || phones[1] != "5547999992222" {
		t.Fatalf("telefones inesperados: %#v", phones)
	}
	if variables["nome"] != "Aluno Teste" || variables["responsavel"] != "Responsável Teste" || variables["nota"] != "82.5" {
		t.Fatalf("variáveis inesperadas: %#v", variables)
	}
}

func TestJovemAprendizHMACSignature(t *testing.T) {
	const key = "segredo-teste"
	body := `{"event_id":"teste-1","type":"exam.completed"}`
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = io.WriteString(mac, body)
	req := httptest.NewRequest("POST", "/api/integrations/jovem-aprendiz/webhook", strings.NewReader(body))
	req.Header.Set("X-Webhook-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	if !verifyJovemAprendizSignature(req, key) {
		t.Fatal("assinatura válida foi rejeitada")
	}
	readBack, _ := io.ReadAll(req.Body)
	if string(readBack) != body {
		t.Fatal("o middleware não restaurou o corpo da requisição")
	}
}
