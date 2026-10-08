package handler

import (
	"github.com/gin-gonic/gin"
	"time"
	"voex-server/internal/filestore"
	"voex-server/internal/shared"
	"voex-server/internal/transport"
)

func (h *Handler) RegisterShareRoutes(optional, protected *gin.RouterGroup) {
	// GET /api/document/:fileKey/shares：有文档分享权限的用户读取分享记录。
	protected.GET("/document/:fileKey/shares", h.shareList)
	// HEAD /api/document/:fileKey/shares：校验分享权限并返回分享列表响应头。
	protected.HEAD("/document/:fileKey/shares", h.shareList)
	// POST /api/document/:fileKey/shares：有文档分享权限的用户创建只读或编辑链接。
	protected.POST("/document/:fileKey/shares", h.shareCreate)
	// DELETE /api/document/:fileKey/shares/:id：项目管理员或分享创建者撤销链接。
	protected.DELETE("/document/:fileKey/shares/:id", h.shareRevoke)

	public := optional.Group("/shared-file", h.shareAccess)
	// GET /api/shared-file：持有效分享令牌的匿名或登录用户读取指定文档。
	public.GET("", h.shareGetDocument)
	// HEAD /api/shared-file：校验有效分享令牌后返回文档响应头。
	public.HEAD("", h.shareGetDocument)
	// PUT /api/shared-file：具备编辑能力的链接持有者按版本号保存正文。
	public.PUT("", h.shareUpdateDocument)
	// GET /api/shared-file/attachments/:hash：持有效分享令牌的用户读取该文档附件。
	public.GET("/attachments/:hash", h.shareGetAttachment)
	// HEAD /api/shared-file/attachments/:hash：校验分享令牌后读取附件响应头。
	public.HEAD("/attachments/:hash", h.shareGetAttachment)
	// GET /api/shared-file/creator-avator：持有效分享令牌的用户读取该文档创建者头像。
	public.GET("/creator-avator", h.shareGetAvatar)
	// HEAD /api/shared-file/creator-avator：校验分享令牌后读取创建者头像响应头。
	public.HEAD("/creator-avator", h.shareGetAvatar)
}

func (h *Handler) shareAccess(c *gin.Context) {
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "no-store")
	h.Limiter.Limit(c.Request, "shared-file", 180, time.Minute)
	c.Next()
}
func (h *Handler) shareList(c *gin.Context) {
	c.JSON(200, h.Service.ListDocumentShares(c.Request.Context(), c.Param("fileKey")))
}
func (h *Handler) shareCreate(c *gin.Context) {
	c.JSON(201, h.Service.CreateDocumentShare(c.Request.Context(), c.Param("fileKey"), transport.Body(c)))
}
func (h *Handler) shareRevoke(c *gin.Context) {
	h.Service.RevokeDocumentShare(c.Request.Context(), c.Param("fileKey"), c.Param("id"))
	c.JSON(200, shared.Row{"success": true})
}
func (h *Handler) shareGetDocument(c *gin.Context) {
	c.JSON(200, h.Service.GetSharedDocument(c.Request.Context(), c.GetHeader("X-Share-Token")))
}
func (h *Handler) shareUpdateDocument(c *gin.Context) {
	c.JSON(200, h.Service.UpdateSharedDocument(c.Request.Context(), c.GetHeader("X-Share-Token"), transport.Body(c)))
}
func (h *Handler) shareGetAttachment(c *gin.Context) {
	file := h.Service.SharedAttachment(c.Request.Context(), c.GetHeader("X-Share-Token"), c.Param("hash"))
	filestore.Serve(c.Writer, c.Request, file)
}
func (h *Handler) shareGetAvatar(c *gin.Context) {
	file := h.Service.SharedAvatar(c.Request.Context(), c.GetHeader("X-Share-Token"))
	filestore.Serve(c.Writer, c.Request, file)
}
