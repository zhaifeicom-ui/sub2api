//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/videostudio"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoAssetHeadAndRange(t *testing.T) {
	for _, tc := range []struct {
		name, method, rangeHeader, body string
		status                          int
	}{
		{"full", http.MethodGet, "", "abcdef", 200},
		{"head", http.MethodHead, "", "", 200},
		{"range", http.MethodGet, "bytes=1-3", "bcd", 206},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery("SELECT mime,content FROM video_studio_assets").WithArgs("asset_test").WillReturnRows(sqlmock.NewRows([]string{"mime", "content"}).AddRow("video/mp4", []byte("abcdef")))
			h := &VideoStudioHandler{store: &videostudio.Store{DB: db}}
			r := gin.New()
			r.GET("/assets/:id", h.Asset)
			r.HEAD("/assets/:id", h.Asset)
			req := httptest.NewRequest(tc.method, "/assets/asset_test", nil)
			if tc.rangeHeader != "" {
				req.Header.Set("Range", tc.rangeHeader)
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, req)
			require.Equal(t, tc.status, recorder.Code)
			require.Equal(t, tc.body, recorder.Body.String())
			require.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
