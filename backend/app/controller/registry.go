package controller

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"

	appmw "github.com/bze-alphateam/bze-scan/backend/app/middleware"
	"github.com/bze-alphateam/bze-scan/backend/internal/assets"
)

// CacheAssetList is the Cache-Control of the asset list routes: the list
// follows the chain within a block or two, the ETag makes revalidation
// free.
const CacheAssetList = "public, max-age=30"

// AssetList serves the asset list from memory; implemented by assets.List.
type AssetList interface {
	// Current is the last build; nil before the first one.
	Current() *assets.Snapshot
}

// RegistryController serves the asset list the dapps read.
type RegistryController struct {
	list AssetList
	log  log.FieldLogger
}

// NewRegistryController returns a controller over list, logging unknown
// denoms to lg.
func NewRegistryController(list AssetList, lg log.FieldLogger) *RegistryController {
	return &RegistryController{list: list, log: lg}
}

// Assets serves GET /api/v1/registry/assets: the whole list, unpaginated.
func (h *RegistryController) Assets(c *echo.Context) error {
	snap := h.list.Current()
	if snap == nil {
		return notBuilt(c)
	}
	return serveVersioned(c, snap.Version, snap.Body)
}

// Asset serves GET /api/v1/registry/assets/{denom}: one asset, its denom
// URL-encoded or not.
func (h *RegistryController) Asset(c *echo.Context) error {
	snap := h.list.Current()
	if snap == nil {
		return notBuilt(c)
	}
	denom, err := url.PathUnescape(c.Param("*"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "denom is not a valid URL-encoded value")
	}
	version, body, ok := snap.Asset(denom)
	if !ok {
		reqID, _ := c.Get(appmw.RequestIDContextKey).(string)
		h.log.WithFields(log.Fields{"denom": denom, "request_id": reqID}).Info("asset list: unknown denom")
		return echo.NewHTTPError(http.StatusNotFound, "no such asset")
	}
	return serveVersioned(c, version, body)
}

// serveVersioned answers body with its version as ETag, or 304 when the
// request already holds it.
func serveVersioned(c *echo.Context, version string, body []byte) error {
	etag := `"` + version + `"`
	h := c.Response().Header()
	h.Set("ETag", etag)
	h.Set(echo.HeaderCacheControl, CacheAssetList)
	if matches(c.Request().Header.Get("If-None-Match"), etag) {
		return c.NoContent(http.StatusNotModified)
	}
	return c.JSONBlob(http.StatusOK, body)
}

// matches reports whether an If-None-Match header names etag (weakly, as
// RFC 9110 compares for GET).
func matches(header, etag string) bool {
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
			return true
		}
	}
	return false
}

// notBuilt answers 503 while the first build has not completed.
func notBuilt(c *echo.Context) error {
	c.Response().Header().Set(echo.HeaderCacheControl, CacheNoStore)
	return c.JSON(http.StatusServiceUnavailable, appmw.ErrorBody{Error: appmw.ErrorDetail{
		Code: "upstream_error", Message: "the asset list is not built yet",
	}})
}
