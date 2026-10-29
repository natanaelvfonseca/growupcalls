package main

import (
	"encoding/json"
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
