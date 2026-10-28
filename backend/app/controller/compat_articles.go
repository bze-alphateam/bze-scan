package controller

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/medium"
)

// ArticleFeed serves the latest Medium articles from memory; implemented by
// medium.Feed.
type ArticleFeed interface {
	Articles(ctx context.Context) []medium.Article
}

// CompatArticlesController serves the aggregator-compatible
// GET /aggregator/api/articles/medium.
type CompatArticlesController struct {
	feed ArticleFeed
}

// NewCompatArticlesController returns a controller over feed.
func NewCompatArticlesController(feed ArticleFeed) *CompatArticlesController {
	return &CompatArticlesController{feed: feed}
}

// Medium serves GET /aggregator/api/articles/medium as the aggregator does: the feed's
// first six articles, always 200. With no read in the last 24 hours the
// list is empty, as the aggregator answers a failing feed.
func (h *CompatArticlesController) Medium(c *echo.Context) error {
	list := h.feed.Articles(c.Request().Context())
	out := make([]dto.CompatArticle, len(list))
	for i, a := range list {
		out[i] = dto.CompatArticle{
			Title: a.Title, URL: a.URL, PictureURL: a.PictureURL,
			Description: a.Description, PublishDate: a.PublishDate, AuthorName: a.AuthorName,
		}
	}
	return c.JSON(http.StatusOK, out)
}
