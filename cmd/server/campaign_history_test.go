package main

import "testing"

func TestCampaignSimilarityMatchesPersonalizedCopies(t *testing.T) {
	a := campaignTokens("Olá, Maria! Você foi aprovada. Escolha 1 14h, 2 16h ou 3 18h. Evento gratuito no Side Vale Office.")
	b := campaignTokens("Olá, Ana! Você foi aprovada. Escolha 1 14h, 2 16h ou 3 18h. Evento gratuito no Side Vale Office.")
	if got := campaignSimilarity(a, b); got < 0.80 {
		t.Fatalf("similaridade insuficiente: %.2f", got)
	}
	c := campaignTokens("Boa noite, o orçamento do adesivo ficará pronto amanhã.")
	if got := campaignSimilarity(a, c); got >= 0.55 {
		t.Fatalf("mensagem comum não pode ser classificada como campanha: %.2f", got)
	}
}
