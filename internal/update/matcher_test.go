package update

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func TestNotTitleFilterIssue798(t *testing.T) {
	// https://github.com/mxpv/podsync/issues/798
	filters := appconfig.Filters{
		NotTitle:    "(?i)^(holy mass|holy sacrifice|the holy)( |$)",
		MinDuration: 600,
	}

	matcher, err := newFilterMatcher(filters)
	require.NoError(t, err)
	// Titles starting with pattern should be excluded
	assert.False(t, matcher.Match(&model.Episode{ID: "1", Title: "Holy Mass — Tuesday", Duration: 3600}))
	assert.False(t, matcher.Match(&model.Episode{ID: "2", Title: "The Holy Sacrifice of the Mass", Duration: 3600}))
	assert.False(t, matcher.Match(&model.Episode{ID: "3", Title: "The Holy Mass (Latin)", Duration: 3600}))

	// Titles NOT starting with pattern should be included
	assert.True(t, matcher.Match(&model.Episode{ID: "4", Title: "Homily: The Parable of the Good Samaritan", Duration: 1200}))
	assert.True(t, matcher.Match(&model.Episode{ID: "5", Title: "Sermon — Love Your Enemies", Duration: 1800}))
	assert.True(t, matcher.Match(&model.Episode{ID: "6", Title: "Reflection on Today's Gospel", Duration: 900}))
}

func TestInvalidFilterFailsClosed(t *testing.T) {
	_, err := newFilterMatcher(appconfig.Filters{Title: "["})
	require.Error(t, err)
}
