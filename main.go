package main

import (
	"log"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"peer-messenger/internal/handlers"
	"peer-messenger/internal/loggers"
)

func main() {
	logger, err := loggers.NewZap()
	if err != nil {
		log.Panic(err)
	}

	validate := validator.New()

	handler := handlers.NewPeerMessenger(logger, validate)

	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		for _, err := range c.Errors {
			logger.Error(err.Error())
		}
	})

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = append(corsConfig.AllowHeaders, "Authorization")
	engine.Use(cors.New(corsConfig))

	engine.Use(func(c *gin.Context) {
		logger.Info("Request received: ", c.Request.URL.Path)
	})

	engine.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, map[string]string{"info": "pong"})
	})

	engine.POST("/register", handler.Register)
	engine.POST("/login", handler.Login)
	engine.POST("/channel/join", handler.JoinChannel)
	engine.GET("/channel/subscribe", handler.Subscribe)
	engine.POST("/peer/send", handler.SendToPeer)

	err = engine.Run(":8080")
	if err != nil {
		logger.Error(err)
	}
}
