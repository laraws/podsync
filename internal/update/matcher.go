package update

import (
	"fmt"
	"regexp"
	"time"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

type filterMatcher struct {
	filters                                      config.Filters
	title, notTitle, description, notDescription *regexp.Regexp
}

// Compile once per update. Invalid patterns fail closed even for direct callers
// that bypass configuration loading.
func newFilterMatcher(filters config.Filters) (*filterMatcher, error) {
	matcher := &filterMatcher{filters: filters}
	for _, entry := range []struct {
		pattern string
		target  **regexp.Regexp
	}{{filters.Title, &matcher.title}, {filters.NotTitle, &matcher.notTitle}, {filters.Description, &matcher.description}, {filters.NotDescription, &matcher.notDescription}} {
		if entry.pattern == "" {
			continue
		}
		expression, err := regexp.Compile(entry.pattern)
		if err != nil {
			return nil, fmt.Errorf("compile episode filter: %w", err)
		}
		*entry.target = expression
	}
	return matcher, nil
}
func (m *filterMatcher) Match(episode *model.Episode) bool {
	if m.title != nil && !m.title.MatchString(episode.Title) || m.notTitle != nil && m.notTitle.MatchString(episode.Title) || m.description != nil && !m.description.MatchString(episode.Description) || m.notDescription != nil && m.notDescription.MatchString(episode.Description) {
		return false
	}
	f := m.filters
	if f.MaxDuration > 0 && episode.Duration > f.MaxDuration || f.MinDuration > 0 && episode.Duration < f.MinDuration {
		return false
	}
	age := time.Since(episode.PubDate)
	if f.MaxAge > 0 && age > time.Duration(f.MaxAge)*24*time.Hour || f.MinAge > 0 && age < time.Duration(f.MinAge)*24*time.Hour {
		return false
	}
	return true
}
