package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"yingce/backend/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterHajimiBridgeRoutes 注册 hajimi 余额互通接口；登录、归属和管理员权限都在 service 校验。
func RegisterHajimiBridgeRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/hajimi-bridge/wallet", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		wallet, err := svc.HajimiBridgeWallet(c.Request.Context(), user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, wallet)
	})
	r.POST("/hajimi-bridge/transfers", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "hajimi-bridge-transfer:"+user.ID, 30, 10*time.Minute) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var req service.HajimiBridgeCreateTransferRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failService(c, service.BadAuthRequest("划转参数无效"))
			return
		}
		transfer, err := svc.CreateHajimiBridgeTransfer(c.Request.Context(), user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, transfer)
	})
	r.GET("/hajimi-bridge/transfers", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, pageSize, err := parsePaginationQuery(c, 20)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.HajimiBridgeTransfers(user, page, pageSize)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.PUT("/hajimi-bridge/auto-topup", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var req service.HajimiBridgeAutoTopupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failService(c, service.BadAuthRequest("自动转入设置参数无效"))
			return
		}
		result, err := svc.UpdateHajimiBridgeAutoTopup(c.Request.Context(), user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.GET("/admin/hajimi-bridge/overview", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		overview, err := svc.AdminHajimiBridgeOverview(c.Request.Context(), user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, overview)
	})
	r.GET("/admin/hajimi-bridge/margin", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		req := service.HajimiBridgeMarginRequest{From: c.Query("from"), To: c.Query("to")}
		if raw := strings.TrimSpace(c.Query("utcOffsetMinutes")); raw != "" {
			offset, err := strconv.Atoi(raw)
			if err != nil {
				failService(c, service.BadAuthRequest("时区偏移无效"))
				return
			}
			req.UTCOffsetMinutes = &offset
		}
		report, err := svc.AdminHajimiBridgeMargin(c.Request.Context(), user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, report)
	})
	r.POST("/admin/hajimi-bridge/transfers/:id/retry", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		transfer, err := svc.AdminRetryHajimiBridgeTransfer(c.Request.Context(), user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, transfer)
	})
}
