package promptstack

import (
	"strconv"
	"strings"
)

const (
	DiffLineUnchanged = "unchanged"
	DiffLineAdded     = "added"
	DiffLineRemoved   = "removed"
)

type DiffLine struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type VersionDiff struct {
	LayerID     string     `json:"layer_id"`
	FromVersion int        `json:"from_version"`
	ToVersion   int        `json:"to_version"`
	Lines       []DiffLine `json:"lines"`
	UnifiedDiff string     `json:"unified_diff"`
}

func buildVersionDiff(layerID string, fromVersion int, fromContent string, toVersion int, toContent string) VersionDiff {
	lines := diffLines(fromContent, toContent)
	return VersionDiff{
		LayerID:     layerID,
		FromVersion: fromVersion,
		ToVersion:   toVersion,
		Lines:       lines,
		UnifiedDiff: renderUnifiedDiff(fromVersion, toVersion, lines),
	}
}

func diffLines(fromContent, toContent string) []DiffLine {
	from := splitContentLines(fromContent)
	to := splitContentLines(toContent)

	n := len(from)
	m := len(to)

	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if from[i] == to[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	result := make([]DiffLine, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case from[i] == to[j]:
			result = append(result, DiffLine{Type: DiffLineUnchanged, Content: from[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			result = append(result, DiffLine{Type: DiffLineRemoved, Content: from[i]})
			i++
		default:
			result = append(result, DiffLine{Type: DiffLineAdded, Content: to[j]})
			j++
		}
	}

	for i < n {
		result = append(result, DiffLine{Type: DiffLineRemoved, Content: from[i]})
		i++
	}
	for j < m {
		result = append(result, DiffLine{Type: DiffLineAdded, Content: to[j]})
		j++
	}

	return result
}

func renderUnifiedDiff(fromVersion, toVersion int, lines []DiffLine) string {
	var b strings.Builder
	b.WriteString("--- version ")
	b.WriteString(strconv.Itoa(fromVersion))
	b.WriteString("\n")
	b.WriteString("+++ version ")
	b.WriteString(strconv.Itoa(toVersion))
	b.WriteString("\n")

	for _, line := range lines {
		switch line.Type {
		case DiffLineAdded:
			b.WriteString("+")
		case DiffLineRemoved:
			b.WriteString("-")
		default:
			b.WriteString(" ")
		}
		b.WriteString(line.Content)
		b.WriteString("\n")
	}

	return b.String()
}

func splitContentLines(content string) []string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if normalized == "" {
		return nil
	}
	parts := strings.Split(normalized, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
