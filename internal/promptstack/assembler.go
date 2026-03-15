package promptstack

import "strings"

const layerMarkerFormat = "<<<LAYER: %s>>>"

func Assemble(stack PromptStack) string {
	var b strings.Builder

	for i, layer := range stack.LayersInOrder() {
		if i > 0 {
			b.WriteString("\n")
		}

		b.WriteString(layerMarker(layer.LayerID))
		b.WriteString("\n")

		b.WriteString(layer.Content)
		if !strings.HasSuffix(layer.Content, "\n") {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func layerMarker(layerID string) string {
	return strings.Replace(layerMarkerFormat, "%s", layerID, 1)
}
