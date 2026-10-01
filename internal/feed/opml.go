package feed

import (
	"fmt"
	"sort"

	"github.com/gilliek/go-opml/opml"
)

// Subscription is fully resolved by the update layer, including storage prefix
// and presentation overrides. Rendering performs no database or storage calls.
type Subscription struct{ ID, Title, Description, URL string }

func BuildOPML(subscriptions []Subscription) (string, error) {
	ordered := append([]Subscription(nil), subscriptions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	doc := opml.OPML{Version: "1.0", Head: opml.Head{Title: "Podsync feeds"}}
	for _, subscription := range ordered {
		doc.Body.Outlines = append(doc.Body.Outlines, opml.Outline{Title: subscription.Title, Text: subscription.Description, Type: "rss", XMLURL: subscription.URL})
	}
	out, err := doc.XML()
	if err != nil {
		return "", fmt.Errorf("marshal OPML: %w", err)
	}
	return out, nil
}
