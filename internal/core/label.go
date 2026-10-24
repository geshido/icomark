package core

import (
	"sort"
	"strings"
)

const commentPrefix = "💬 "

// Label is the structured form of a visible backend label.
type Label struct {
	Base        string
	StatusIndex int
	HasComment  bool
}

// ParseLabel removes the longest matching configured status prefix and then
// the optional comment marker. Labels without a status prefix use status zero.
func ParseLabel(visible string, cfg Config) Label {
	parsed := Label{Base: visible}
	type candidate struct {
		index  int
		prefix string
	}
	candidates := make([]candidate, 0, len(cfg.Statuses))
	for i, status := range cfg.Statuses {
		if status.Prefix != "" {
			candidates = append(candidates, candidate{index: i, prefix: status.Prefix})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return len(candidates[i].prefix) > len(candidates[j].prefix)
	})
	for _, status := range candidates {
		if strings.HasPrefix(parsed.Base, status.prefix) {
			parsed.StatusIndex = status.index
			parsed.Base = strings.TrimPrefix(parsed.Base, status.prefix)
			break
		}
	}
	if strings.HasPrefix(parsed.Base, commentPrefix) {
		parsed.HasComment = true
		parsed.Base = strings.TrimPrefix(parsed.Base, commentPrefix)
	}
	return parsed
}

// BuildLabel composes a visible label. An out-of-range status is treated as
// having no prefix rather than panicking.
func BuildLabel(base string, status int, hasComment bool, cfg Config) string {
	var b strings.Builder
	if status >= 0 && status < len(cfg.Statuses) {
		b.WriteString(cfg.Statuses[status].Prefix)
	}
	if hasComment {
		b.WriteString(commentPrefix)
	}
	b.WriteString(base)
	return b.String()
}
