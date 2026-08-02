package ai

import (
	"fmt"
	"strings"
)

// toolName is the single tool/function the model must call, which forces a
// structured {category_id, confidence} result instead of free-form text.
const toolName = "suggest_category"

// candidateList returns the candidate ids (for the schema enum) and a
// human-readable list (for the prompt).
func candidateList(req SuggestRequest) (ids []string, text string) {
	var b strings.Builder
	ids = make([]string, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		ids = append(ids, c.ID.String())
		fmt.Fprintf(&b, "- %s (id: %s)\n", c.Name, c.ID.String())
	}
	return ids, b.String()
}

// buildPrompt renders the categorization instruction shared by all providers.
func buildPrompt(req SuggestRequest, candidateText string) string {
	return fmt.Sprintf(
		"Eres un clasificador de gastos personales para una app colombiana (COP). "+
			"Elige la categoría que mejor corresponde al gasto y responde ÚNICAMENTE llamando "+
			"a la herramienta %q. Si ninguna encaja bien, elige la más cercana con baja confianza.\n\n"+
			"Gasto:\n- descripción: %s\n- monto: %s\n- comercio: %s\n\n"+
			"Categorías disponibles:\n%s",
		toolName, orNA(req.Description), orNA(req.Amount), orNA(req.Merchant), candidateText,
	)
}

func orNA(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no especificado)"
	}
	return s
}
