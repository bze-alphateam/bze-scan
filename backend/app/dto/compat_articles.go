package dto

import "time"

// CompatArticle is one entry of GET /aggregator/api/articles/medium: a Medium article,
// its date as RFC 3339 in UTC.
type CompatArticle struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	PictureURL  string    `json:"picture_url"`
	Description string    `json:"description"`
	PublishDate time.Time `json:"publish_date"`
	AuthorName  string    `json:"author_name"`
}
