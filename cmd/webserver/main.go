// webserver 是 HTTP API 服务入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"live-mixer/docs"
	"live-mixer/internal/bootstrap"
	"live-mixer/internal/config"
	v1handler "live-mixer/internal/handler/v1"
	v2handler "live-mixer/internal/handler/v2"
	"live-mixer/internal/middleware"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/storage"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"
	"live-mixer/internal/service"
	routesv1 "live-mixer/internal/routes/v1"
	routesv2 "live-mixer/internal/routes/v2"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "", "外部配置文件路径（可选）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	logger, err := bootstrap.InitLogger(cfg.Logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync() //nolint:errcheck

	db, err := bootstrap.InitDatabase(cfg.Database, logger)
	if err != nil {
		logger.Fatal("初始化数据库失败", zap.Error(err))
	}

	accountRepo := repository.NewAccountRepository(db)
	liveMaterialRepo := repository.NewLiveMaterialRepository(db)
	llmSystemPromptRepo := repository.NewLLMSystemPromptRepository(db)
	videoProjectRepo := repository.NewVideoProjectRepository(db)
	taskRepo := repository.NewTaskRepository(db)
	accountService := service.NewAccountService(accountRepo)
	authService := service.NewAuthService(accountRepo, cfg.JWT.Secret, cfg.JWT.ExpiresIn)
	asrService := service.NewASRServiceFromConfig(cfg.ASR.ASRClientConfig())

	storageClient, err := storage.NewClientFromAppConfig(cfg.Storage)
	if err != nil {
		logger.Fatal("初始化对象存储失败", zap.Error(err))
	}
	audioPreparer := service.NewLiveMaterialASRAudioPreparer(nil, nil, storageClient, "", logger)
	liveMaterialASRWorker := service.NewLiveMaterialASRWorker(
		liveMaterialRepo,
		asrService,
		audioPreparer,
		logger,
		cfg.Worker.ASRConcurrencyOrDefault(),
	)
	liveMaterialService := service.NewLiveMaterialService(liveMaterialRepo, liveMaterialASRWorker)
	llmSystemPromptService := service.NewLLMSystemPromptService(llmSystemPromptRepo)
	videoProjectService := service.NewVideoProjectService(videoProjectRepo, liveMaterialRepo)

	// OpenAI 兼容协议 LLM 客户端，供 AI 切片 Worker 与同步对话接口共用。
	llmClient := llm.NewClient(cfg.LLM.LLMClientConfig())
	chatService := service.NewChatService(llmClient, logger)
	aiSliceWorker := service.NewAISliceWorker(
		taskRepo,
		liveMaterialRepo,
		videoProjectRepo,
		llmClient,
		logger,
		cfg.Worker.AISliceConcurrencyOrDefault(),
	)

	// 剪映草稿 Worker：ffmpeg 切片 + capcut-mate 组装草稿，进度写入 task 表。
	capcutClient := capcutmate.NewClient(cfg.CapCutMate.CapCutMateClientConfig())
	draftWorker := service.NewDraftWorker(service.DraftWorkerDeps{
		TaskRepo:         taskRepo,
		LiveMaterialRepo: liveMaterialRepo,
		VideoProjectRepo: videoProjectRepo,
		CapCut:           capcutClient,
		Web: webroot.Config{
			RootDir: cfg.Web.RootDir,
			RootURL: cfg.Web.RootURL,
		},
		Logger:      logger,
		Concurrency: cfg.Worker.DraftConcurrencyOrDefault(),
	})
	aiSliceDraftWorker := service.NewAISliceDraftWorker(
		taskRepo,
		videoProjectRepo,
		aiSliceWorker,
		draftWorker,
		logger,
		cfg.Worker.AISliceDraftConcurrencyOrDefault(),
	)
	taskService := service.NewTaskService(taskRepo, liveMaterialRepo, videoProjectRepo, llmSystemPromptRepo, aiSliceWorker, draftWorker, aiSliceDraftWorker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	liveMaterialASRWorker.Start(ctx)
	aiSliceWorker.Start(ctx)
	draftWorker.Start(ctx)
	aiSliceDraftWorker.Start(ctx)
	v1AccountHandler := v1handler.NewAccountHandler(accountService)
	v1AuthHandler := v1handler.NewAuthHandler(authService)
	v1ASRHandler := v1handler.NewASRHandler(asrService)
	v1ChatHandler := v1handler.NewChatHandler(chatService)
	v1LiveMaterialHandler := v1handler.NewLiveMaterialHandler(liveMaterialService, accountRepo)
	v1LLMSystemPromptHandler := v1handler.NewLLMSystemPromptHandler(llmSystemPromptService, accountRepo)
	v1VideoProjectHandler := v1handler.NewVideoProjectHandler(videoProjectService, accountRepo)
	v1TaskHandler := v1handler.NewTaskHandler(taskService, accountRepo)
	v2AccountHandler := v2handler.NewAccountHandler(accountService)

	gin.SetMode(cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestLogger(logger))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/openapi/live-mixer")
	routesv1.RegisterRoutes(api.Group("/v1"), v1AccountHandler, v1AuthHandler, v1ASRHandler, v1LiveMaterialHandler, v1LLMSystemPromptHandler, v1VideoProjectHandler, v1TaskHandler, v1ChatHandler, cfg.JWT.Secret)
	routesv2.RegisterRoutes(api.Group("/v2"), v2AccountHandler, cfg.JWT.Secret)

	docs.SwaggerInfo.Host = cfg.Server.Addr()
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	addr := cfg.Server.Addr()
	logger.Info("HTTP 服务启动", zap.String("addr", addr))
	if err := r.Run(addr); err != nil {
		logger.Fatal("HTTP 服务异常退出", zap.Error(err))
	}
}
