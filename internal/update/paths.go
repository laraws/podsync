package update

import (
	"fmt"

	"github.com/mxpv/podsync/internal/config"
	"github.com/mxpv/podsync/internal/model"
)

func (u *Updater) episodeObjectKey(feedConfig *config.Feed, episode *model.Episode) string {
	if episode.ObjectKey != "" {
		return episode.ObjectKey
	}
	logicalName := fmt.Sprintf("%s/%s", feedConfig.ID, episode.ID+"."+feedConfig.EpisodeExtension())
	return u.storage.ObjectKey(logicalName)
}
