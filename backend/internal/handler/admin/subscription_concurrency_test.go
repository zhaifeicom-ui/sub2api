package admin

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type concurrencySubscriptionRepo struct {
	service.UserSubscriptionRepository
	limit int
}

func (r *concurrencySubscriptionRepo) GetByID(context.Context, int64) (*service.UserSubscription, error) {
	return &service.UserSubscription{ID: 1, UserID: 2, GroupID: 3, ConcurrencyLimit: r.limit}, nil
}
func (r *concurrencySubscriptionRepo) UpdateConcurrencyLimit(_ context.Context, _ int64, limit int) error {
	r.limit = limit
	return nil
}

func TestSubscriptionConcurrencyAdminValidation(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{}`, 400}, {`{"concurrency_limit":null}`, 400}, {`{"concurrency_limit":-1}`, 400}, {`{"concurrency_limit":100001}`, 400}, {`{"concurrency_limit":1.5}`, 400}, {`{"concurrency_limit":2}`, 200}, {`{"concurrency_limit":0}`, 200},
	} {
		t.Run(tc.body, func(t *testing.T) {
			repo := &concurrencySubscriptionRepo{}
			svc := service.NewSubscriptionService(nil, repo, nil, nil, &config.Config{})
			h := NewSubscriptionHandler(svc)
			router := gin.New()
			router.PUT("/subscriptions/:id/concurrency", h.UpdateConcurrency)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("PUT", "/subscriptions/1/concurrency", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}
