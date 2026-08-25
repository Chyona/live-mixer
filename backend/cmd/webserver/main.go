// HTTP API 与后台 Worker 入口。
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"live-mixer/internal/bootstrap"
	"live-mixer/internal/config"
	"live-mixer/internal/draft"
	v1handler "live-mixer/internal/handler/v1"
	v2handler "live-mixer/internal/handler/v2"
	"live-mixer/internal/middleware"
	"live-mixer/internal/pkg/capcutmate"
	"live-mixer/internal/pkg/llm"
	"live-mixer/internal/pkg/media"
	"live-mixer/internal/pkg/storage"
	"live-mixer/internal/pkg/webroot"
	"live-mixer/internal/repository"
	v1routes "live-mixer/internal/routes/v1"
	v2routes "live-mixer/internal/routes/v2"
	"live-mixer/internal/scheduler"
	"live-mixer/internal/service"

	_ "live-mixer/docs"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "", "外部配置文件路径（可选；否则用内嵌 config + 环境变量）")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		panic(err)
	}

	logger, err := bootstrap.InitLogger(cfg.Logger)
	if err != nil {
		panic(err)
	}
	defer logger.Sync() //nolint:errcheck

	db, err := bootstrap.InitDatabase(cfg.Database, logger)
	if err != nil {
		logger.Fatal("初始化数据库失败", zap.Error(err))
	}

	storageClient, err := storage.NewClientFromAppConfig(cfg.Storage)
	if err != nil {
		logger.Fatal("初始化对象存储失败", zap.Error(err))
	}

	accountRepo := repository.NewAccountRepository(db)
	liveMaterialRepo := repository.NewLiveMaterialRepository(db)
	videoProjectRepo := repository.NewVideoProjectRepository(db)
	llmPromptRepo := repository.NewLLMSystemPromptRepository(db)
	taskRepo := repository.NewTaskRepository(db)

	rewriter := cfg.Download.URLRewriter()
	downloader := service.NewFileDownloader(logger, rewriter)
	web := webroot.Config{RootDir: cfg.Web.RootDir}

	asrService := service.NewASRServiceFromConfig(cfg.ASR.ASRClientConfig())
	asrLLM := llm.NewClient(cfg.LLM.LLMClientConfigForASR())
	sliceLLM := llm.NewClient(cfg.LLM.LLMClientConfig())
	chatLLM := llm.NewClient(cfg.LLM.LLMClientConfig())

	audioPreparer := service.NewLiveMaterialASRAudioPreparer(
		downloader,
		media.NewFFmpegConverter(""),
		storageClient,
		"temp",
		logger,
		media.NewFFprobeProber(""),
	)
	asrWorker := service.NewLiveMaterialASRWorker(
		liveMaterialRepo,
		asrService,
		audioPreparer,
		asrLLM,
		logger,
		cfg.Worker.ASRConcurrencyOrDefault(),
		cfg.Worker.ASRStaleTimeout(),
		web,
	)

	capcutClient := capcutmate.NewClient(cfg.CapCutMate.CapCutMateClientConfig())
	generator := draft.NewGenerator(draft.GeneratorDeps{
		CapCut:     capcutClient,
		Cutter:     media.NewFFmpegConverter(""),
		Downloader: downloader,
		Uploader:   storageClient,
		Logger:     logger,
	})
	enableGenVideo := cfg.CapCutMate.GenVideoEnabled()
	draftWorker := service.NewDraftWorker(service.DraftWorkerDeps{
		TaskRepo:         taskRepo,
		LiveMaterialRepo: liveMaterialRepo,
		VideoProjectRepo: videoProjectRepo,
		Generator:        generator,
		VideoExporter:    capcutClient,
		EnableGenVideo:   &enableGenVideo,
		Web:              web,
		Logger:           logger,
		Concurrency:      cfg.Worker.DraftConcurrencyOrDefault(),
		StaleTimeout:     cfg.Worker.DraftStaleTimeout(),
	})
	aiSliceWorker := service.NewAISliceWorker(
		taskRepo,
		liveMaterialRepo,
		videoProjectRepo,
		sliceLLM,
		logger,
		cfg.Worker.AISliceConcurrencyOrDefault(),
		cfg.Worker.AISliceStaleTimeout(),
		web,
	)
	aiSliceDraftWorker := service.NewAISliceDraftWorker(
		taskRepo,
		videoProjectRepo,
		aiSliceWorker,
		draftWorker,
		logger,
		cfg.Worker.AISliceDraftConcurrencyOrDefault(),
		cfg.Worker.AISliceDraftStaleTimeout(),
	)

	accountService := service.NewAccountService(accountRepo)
	authService := service.NewAuthService(accountRepo, cfg.JWT.Secret, cfg.JWT.ExpiresIn)
	liveMaterialService := service.NewLiveMaterialService(liveMaterialRepo, asrWorker)
	llmPromptService := service.NewLLMSystemPromptService(llmPromptRepo)
	videoProjectService := service.NewVideoProjectServiceWithLogger(videoProjectRepo, liveMaterialRepo, logger)
	taskService := service.NewTaskService(
		taskRepo,
		liveMaterialRepo,
		videoProjectRepo,
		llmPromptRepo,
		aiSliceWorker,
		draftWorker,
		aiSliceDraftWorker,
	)
	chatService := service.NewChatService(chatLLM, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	asrWorker.Start(ctx)
	aiSliceWorker.Start(ctx)
	draftWorker.Start(ctx)
	aiSliceDraftWorker.Start(ctx)

	sched := scheduler.New(logger)
	sched.Register(scheduler.Job{
		Name:     "cleanup-staging",
		Interval: cfg.Web.StagingCleanupInterval(),
		Run: func(context.Context) {
			removed, cleanErr := webroot.CleanupStaging(cfg.Web.RootDir, cfg.Web.StagingMaxDirs)
			if cleanErr != nil {
				logger.Warn("清理 staging 失败", zap.Error(cleanErr), zap.Int("removed", removed))
				return
			}
			if removed > 0 {
				logger.Info("已清理过期 staging 目录", zap.Int("removed", removed))
			}
		},
	})
	sched.Register(scheduler.Job{
		Name:     "cleanup-asr-staging",
		Interval: cfg.Web.StagingCleanupInterval(),
		Run: func(context.Context) {
			removed, cleanErr := webroot.CleanupASRStaging(cfg.Web.RootDir, cfg.Web.ASRStagingMaxDirs)
			if cleanErr != nil {
				logger.Warn("清理 ASR staging 失败", zap.Error(cleanErr), zap.Int("removed", removed))
				return
			}
			if removed > 0 {
				logger.Info("已清理过期 ASR staging 目录", zap.Int("removed", removed))
			}
		},
	})
	sched.Start(ctx)

	gin.SetMode(cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestLogger(logger))
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := r.Group("/openapi/live-mixer")
	v1routes.RegisterRoutes(
		api.Group("/v1"),
		v1handler.NewAccountHandler(accountService),
		v1handler.NewAuthHandler(authService),
		v1handler.NewASRHandler(asrService),
		v1handler.NewLiveMaterialHandler(liveMaterialService, accountRepo),
		v1handler.NewLLMSystemPromptHandler(llmPromptService, accountRepo),
		v1handler.NewVideoProjectHandler(videoProjectService, accountRepo),
		v1handler.NewTaskHandler(taskService, accountRepo),
		v1handler.NewChatHandler(chatService),
		cfg.JWT.Secret,
	)
	v2routes.RegisterRoutes(
		api.Group("/v2"),
		v2handler.NewAccountHandler(accountService),
		cfg.JWT.Secret,
	)

	srv := &http.Server{
		Addr:              cfg.Server.Addr(),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		logger.Info("HTTP 服务已启动", zap.String("addr", cfg.Server.Addr()))
		if serveErr := srv.ListenAndServe(); serveErr != nil && serveErr != http.ErrServerClosed {
			logger.Fatal("HTTP 服务异常退出", zap.Error(serveErr))
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP 服务关闭失败", zap.Error(err))
	}
	logger.Info("服务已退出")
}
