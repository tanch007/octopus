package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/probe").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/model", http.MethodPost).
				Handle(probeModel),
		).
		AddRoute(
			router.NewRoute("/group", http.MethodPost).
				Handle(probeGroup),
		)
}

// probeModel 直连指定渠道下的某个模型与凭据测试上游。
// 失败不是请求错误: 上游报错是探测要给出的结论, 故一律按 200 返回结果, 由界面展示失败详情。
func probeModel(c *gin.Context) {
	var request model.ProbeModelRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if request.ChannelID <= 0 || request.ModelName == "" {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	resp.Success(c, relay.ProbeModel(c.Request.Context(), request))
}

// probeGroup 按分组的路由规则挑一个成员来测, 看真实请求此刻会落到谁身上。
func probeGroup(c *gin.Context) {
	var request model.ProbeGroupRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if request.GroupID <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	resp.Success(c, relay.ProbeGroup(c.Request.Context(), request))
}
