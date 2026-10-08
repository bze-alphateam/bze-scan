package controller_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
)

func TestHealthAnswersOKWithEmptyBody(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	require.NoError(t, controller.NewHealthController().Health(e.NewContext(req, rec)))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.Bytes())
}
