package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type qualityHandlerRepoStub struct {
	service.GroupQualityRepository
	access  string
	enabled bool
}

func (r qualityHandlerRepoStub) GetConfig(context.Context, int64) (*service.GroupQualityConfig, error) {
	return &service.GroupQualityConfig{Enabled: r.enabled, CustomerAccess: r.access}, nil
}
func (r qualityHandlerRepoStub) GetRun(context.Context, int64) (*service.GroupQualityRun, error) {
	return &service.GroupQualityRun{ID: 1, GroupID: 7, Source: "private artifact"}, nil
}
func qualityHandlerContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", path, nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	return c, w
}
func TestQualityUserPermissionMatrix(t *testing.T) {
	for _, tc := range []struct {
		access  string
		enabled bool
		run     bool
		want    bool
	}{
		{"hidden", true, false, false}, {"view", true, false, true}, {"view", true, true, false}, {"run", true, true, true}, {"run", false, false, false},
	} {
		c, w := qualityHandlerContext("/")
		svc := service.NewGroupQualityService(qualityHandlerRepoStub{access: tc.access, enabled: tc.enabled}, nil, nil, nil, nil, nil, nil)
		h := &GroupQualityHandler{service: svc, groups: &channelMonitorV2GroupAuthorizerStub{groups: []service.Group{{ID: 7}}}}
		require.Equal(t, tc.want, h.authorize(c, 7, tc.run))
		if !tc.want {
			require.Equal(t, 403, w.Code)
		}
	}
}
func TestQualityUserCannotFetchOtherGroupArtifact(t *testing.T) {
	c, w := qualityHandlerContext("/quality-tests/runs/1")
	c.Params = gin.Params{{Key: "id", Value: "1"}}
	svc := service.NewGroupQualityService(qualityHandlerRepoStub{access: "run", enabled: true}, nil, nil, nil, nil, nil, nil)
	h := &GroupQualityHandler{service: svc, groups: &channelMonitorV2GroupAuthorizerStub{groups: []service.Group{{ID: 8}}}}
	h.Detail(c)
	require.Equal(t, 403, w.Code)
	require.NotContains(t, w.Body.String(), "private artifact")
}
func TestQualityConfigurationRequiresAdminInHandler(t *testing.T) {
	c, w := qualityHandlerContext("/quality-tests/groups/7/config")
	(&GroupQualityHandler{}).SaveConfig(c)
	require.Equal(t, 403, w.Code)
}
func TestQualityCustomerResultRedaction(t *testing.T) {
	id := int64(19)
	r := service.GroupQualityRun{AccountID: &id, RequestedBy: &id, UpstreamModel: "private-alias", Source: "<svg></svg>"}
	redactQualityRun(&r)
	require.Nil(t, r.AccountID)
	require.Nil(t, r.RequestedBy)
	require.Empty(t, r.UpstreamModel)
	require.Equal(t, "<svg></svg>", r.Source)
}
