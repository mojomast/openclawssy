package tools

// paginate standardizes pagination logic for list-type tools.
func paginate[T any](items []T, args map[string]any, defaultLimit, maxLimit int) ([]T, map[string]any) {
	total := len(items)

	limit := getIntArg(args, "limit", defaultLimit)
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	offset := getIntArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return items[offset:end], map[string]any{
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"count":  end - offset,
	}
}
