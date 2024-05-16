package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"peer-messenger/internal/handlers"
	"peer-messenger/internal/metrics"
)

func main() {
	logger, err := NewZap()
	if err != nil {
		log.Panic(err)
	}

	validate := validator.New()

	prom := metrics.New()

	handler := handlers.NewPeerMessenger(logger, validate, prom)

	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, err := range c.Errors {
			logger.Error("got post process error", zap.Error(err))
		}
	})

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = append(corsConfig.AllowHeaders, "Authorization")
	engine.Use(cors.New(corsConfig))

	engine.Use(func(c *gin.Context) {
		startTime := time.Now()
		defer func() {
			prom.RequestDuration.WithLabelValues(c.Request.URL.Path).Observe(time.Since(startTime).Seconds())
		}()

		defer func() {
			prom.RPS.WithLabelValues(c.Request.URL.Path).Inc()
		}()

		c.Next()
	})

	engine.Use(func(c *gin.Context) {
		reqBodyCopy := &bytes.Buffer{}
		_, err = io.Copy(reqBodyCopy, c.Request.Body)
		if err != nil {
			logger.Error("can't copy request body", zap.Error(err))
			c.Abort()
			return
		}

		c.Request.Body = io.NopCloser(reqBodyCopy)
		logger.Info("Request received", zap.String("path", c.Request.URL.Path), zap.String("body", reqBodyCopy.String()))

		c.Next()
		logger.Info("Request processed", zap.String("path", c.Request.URL.Path))
	})

	engine.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, map[string]string{"info": "pong"})
	})

	engine.POST("/register", handler.Register)
	engine.POST("/login", handler.Login)
	engine.POST("/channel/join", handler.JoinChannel)
	engine.POST("channel/leave", handler.LeaveChannel)
	engine.GET("/channel/subscribe", handler.Subscribe)
	engine.POST("/channel/collect", handler.CollectMessages)
	engine.POST("/peer/send", handler.SendToPeer)
	engine.DELETE("/room/delete", handler.RemoveRoom)
	engine.POST("/metrics/resolution", handler.CollectResolution)

	err = engine.Run(":8080")
	if err != nil {
		logger.Error("cannot start gin engine", zap.Error(err))
	}
}

func NewZap() (*zap.Logger, error) {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig = encoderConfig
	logConfig.DisableStacktrace = true

	level := zapcore.DebugLevel
	logConfig.Level.SetLevel(level)

	coreLogger, err := logConfig.Build()
	if err != nil {
		return nil, err
	}

	return coreLogger, nil
}
