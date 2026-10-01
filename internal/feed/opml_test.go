package feed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOPML(t *testing.T) {
	subscriptions := []Subscription{{ID: "b", Title: "Second", URL: "https://media.example.com/prefix/b.xml"}, {ID: "a", Title: "First", Description: "desc", URL: "https://media.example.com/prefix/a.xml"}}
	result, err := BuildOPML(subscriptions)
	require.NoError(t, err)
	assert.Contains(t, result, `xmlUrl="https://media.example.com/prefix/a.xml"`)
	assert.Contains(t, result, `title="First"`)
	again, err := BuildOPML([]Subscription{subscriptions[1], subscriptions[0]})
	require.NoError(t, err)
	assert.Equal(t, result, again)
	assert.Equal(t, "b", subscriptions[0].ID)
}
