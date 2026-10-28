package serve

import (
	"net/http"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/medium"
)

// mediumTimeout bounds one read of the Medium feed.
const mediumTimeout = 10 * time.Second

// NewArticleFeed returns the Medium feed behind /aggregator/api/articles/medium; nil
// without a feed URL.
func NewArticleFeed(cfg *config.Config) *medium.Feed {
	if cfg.MediumFeedURL == "" {
		return nil
	}
	client := medium.New(&http.Client{Timeout: mediumTimeout}, cfg.MediumFeedURL)
	return medium.NewFeed(medium.FeedDeps{Source: client})
}
