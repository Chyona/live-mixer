// Package v1 注册 API v1 路由分组。
package v1

import (
	v1handler "live-mixer/internal/handler/v1"
	"live-mixer/internal/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes 注册 v1 版本全部路由。
func RegisterRoutes(rg *gin.RouterGroup, accountHandler *v1handler.AccountHandler, authHandler *v1handler.AuthHandler, asrHandler *v1handler.ASRHandler, liveMaterialHandler *v1handler.LiveMaterialHandler, llmSystemPromptHandler *v1handler.LLMSystemPromptHandler, jwtSecret string) {
	// 登录接口无需 JWT 鉴权
	rg.POST("/auth/login", authHandler.Login)
	// ASR 同步识别接口无需 JWT 鉴权
	rg.POST("/asr", asrHandler.Transcribe)

	// 其余 v1 接口均需 JWT 鉴权
	authorized := rg.Group("", middleware.JWTAuth(jwtSecret))

	accounts := authorized.Group("/accounts")
	{
		accounts.POST("", accountHandler.CreateAccount)
		accounts.GET("", accountHandler.ListAccounts)
		accounts.GET("/:id", accountHandler.GetAccount)
		accounts.PUT("/:id", accountHandler.UpdateAccount)
		accounts.DELETE("/:id", accountHandler.DeleteAccount)
	}

	liveMaterials := authorized.Group("/live-materials")
	{
		liveMaterials.GET("", liveMaterialHandler.ListLiveMaterials)
		liveMaterials.POST("", liveMaterialHandler.CreateLiveMaterial)
		liveMaterials.GET("/:id", liveMaterialHandler.GetLiveMaterial)
		liveMaterials.PUT("/:id", liveMaterialHandler.UpdateLiveMaterial)
	}

	llmSystemPrompts := authorized.Group("/llm-system-prompts")
	{
		llmSystemPrompts.GET("", llmSystemPromptHandler.ListLLMSystemPrompts)
		llmSystemPrompts.POST("", llmSystemPromptHandler.CreateLLMSystemPrompt)
		llmSystemPrompts.GET("/:id", llmSystemPromptHandler.GetLLMSystemPrompt)
		llmSystemPrompts.PUT("/:id", llmSystemPromptHandler.UpdateLLMSystemPrompt)
		llmSystemPrompts.DELETE("/:id", llmSystemPromptHandler.DeleteLLMSystemPrompt)
	}
}
