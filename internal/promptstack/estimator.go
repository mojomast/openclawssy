package promptstack

import (
	"math"
	"strings"
)

const (
	estimationMethodWordCountHeuristicLabel = "word_count_x1.3"
	TokenEstimationMethodWordCountHeuristic = estimationMethodWordCountHeuristicLabel
)

type LayerTokenEstimate struct {
	LayerID    string `json:"layer_id"`
	WordCount  int    `json:"word_count"`
	TokenCount int    `json:"token_count"`
}

type TokenEstimate struct {
	PerLayer         []LayerTokenEstimate `json:"per_layer"`
	TotalTokens      int                  `json:"total_tokens"`
	EstimationMethod string               `json:"estimation_method"`
}

func EstimateTokens(stack PromptStack) TokenEstimate {
	layers := stack.LayersInOrder()
	perLayer := make([]LayerTokenEstimate, 0, len(layers))
	total := 0

	for _, layer := range layers {
		words := countWords(layer.Content)
		tokens := estimateWordTokens(words)
		perLayer = append(perLayer, LayerTokenEstimate{
			LayerID:    layer.LayerID,
			WordCount:  words,
			TokenCount: tokens,
		})
		total += tokens
	}

	return TokenEstimate{
		PerLayer:         perLayer,
		TotalTokens:      total,
		EstimationMethod: estimationMethodWordCountHeuristicLabel,
	}
}

func countWords(content string) int {
	return len(strings.Fields(content))
}

func estimateWordTokens(words int) int {
	if words <= 0 {
		return 0
	}
	return int(math.Ceil(float64(words) * 1.3))
}
