package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"peer-messenger/internal"
	"peer-messenger/internal/metrics"
	"peer-messenger/internal/models"
)

type PeerMessenger struct {
	logger            *zap.Logger
	salt              []byte
	validate          *validator.Validate
	users             map[string]struct{}
	roomRepo          *internal.RoomRepository
	roomKeysExtractor *regexp.Regexp
	metrics           *metrics.Metrics
}

func NewPeerMessenger(logger *zap.Logger, validate *validator.Validate, metrics *metrics.Metrics) *PeerMessenger {
	salt := []byte("asasasas")

	out := &PeerMessenger{
		logger:            logger,
		salt:              salt,
		validate:          validate,
		users:             make(map[string]struct{}),
		roomRepo:          internal.NewRoomRepository(logger, metrics),
		roomKeysExtractor: regexp.MustCompile(`[^_]+`),
		metrics:           metrics,
	}

	g := new(errgroup.Group)

	g.Go(func() error {
		for {
			time.Sleep(10 * time.Second)
			out.roomRepo.Clean()

			state := out.roomRepo.GetState()
			logger.Debug("rooms state collected", zap.Any("state", state))
		}
	})

	return out
}

func (handler *PeerMessenger) Register(c *gin.Context) {
	c.AbortWithStatus(http.StatusNotImplemented)
}

func (handler *PeerMessenger) Login(c *gin.Context) {
	dto, err := getTypedRequestBody[models.LoginRequest](c.Request.Body, handler.validate)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	// add temporal user for now
	handler.users[dto.UserID] = struct{}{}

	token := dto.UserID + string(handler.salt)
	c.JSON(http.StatusOK, models.LoginResponse{Token: token})
}

func (handler *PeerMessenger) JoinChannel(c *gin.Context) {
	userID, err := handler.extractUserID(c)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	if _, ok := handler.users[userID]; !ok {
		err := errors.New("user does not exist")
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	dto, err := getTypedRequestBody[models.ChannelRequest](c.Request.Body, handler.validate)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var (
		roomName = dto.ChannelName
		room     *internal.Room
	)
	if !handler.roomRepo.Exist(roomName) {
		room, err = handler.roomRepo.AddRoom(roomName)
	} else {
		room, err = handler.roomRepo.Get(roomName)
	}
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	err = room.AddUser(userID)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	subscriptionID := fmt.Sprintf("%s__%s", dto.ChannelName, userID)

	c.JSON(http.StatusOK, map[string]string{"subscriptionID": subscriptionID})
}

func (handler *PeerMessenger) LeaveChannel(c *gin.Context) {
	userID, err := handler.extractUserID(c)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	if _, ok := handler.users[userID]; !ok {
		err := errors.New("user does not exist")
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	dto, err := getTypedRequestBody[models.ChannelRequest](c.Request.Body, handler.validate)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	room, err := handler.roomRepo.Get(dto.ChannelName)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	err = room.RemoveUser(userID)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, map[string]string{"status": "OK"})
}

func (handler *PeerMessenger) Subscribe(c *gin.Context) {
	subscriptionID, ok := c.GetQuery("subscriptionID")
	if !ok || subscriptionID == "" {
		err := errors.New("subscriptionID is not provided")
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	keys := handler.roomKeysExtractor.FindAllString(subscriptionID, 2)
	roomKey, userID := keys[0], keys[1]

	room, err := handler.roomRepo.Get(roomKey)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	userChan, err := room.GetUserEventsChan(userID)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Content-Type", "text/event-stream")

	for entity := range userChan {
		c.SSEvent("message", entity)
		c.Writer.Flush()
	}

	handler.logger.Info(
		"leaving from event subscription",
		zap.String("room", roomKey),
		zap.String("user", userID),
	)

	c.AbortWithStatus(http.StatusNoContent)
}

func (handler *PeerMessenger) CollectMessages(c *gin.Context) {
	subscriptionID, ok := c.GetQuery("subscriptionID")
	if !ok || subscriptionID == "" {
		err := errors.New("subscriptionID is not provided")
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	keys := handler.roomKeysExtractor.FindAllString(subscriptionID, 2)
	roomKey, userID := keys[0], keys[1]

	room, err := handler.roomRepo.Get(roomKey)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	entities, err := room.GetUserEventsSlice(userID)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, map[string][]models.ChannelEntity{"entities": entities})
}

func (handler *PeerMessenger) SendToPeer(c *gin.Context) {
	userID, err := handler.extractUserID(c)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	if _, ok := handler.users[userID]; !ok {
		err := errors.New("user does not exist")
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	dto, err := getTypedRequestBody[models.SendToPeerRequest](c.Request.Body, handler.validate)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	room, err := handler.roomRepo.Get(dto.ChannelName)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	err = room.SendToUser(c.Request.Context(), userID, dto.DestinationUserID, dto.Message)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	c.AbortWithStatus(http.StatusOK)
}

func (handler *PeerMessenger) RemoveRoom(c *gin.Context) {
	dto, err := getTypedRequestBody[models.ChannelRequest](c.Request.Body, handler.validate)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	handler.roomRepo.RemoveRoom(dto.ChannelName)

	c.AbortWithStatus(http.StatusOK)
}

func (handler *PeerMessenger) extractUserID(c *gin.Context) (string, error) {
	token := c.GetHeader("Authorization")
	if token == "" {
		return "", errors.New("header Authorization is empty")
	}

	lastIndex := len(token) - len(handler.salt)
	return token[:lastIndex], nil
}

func getTypedRequestBody[T any](body io.Reader, validate *validator.Validate) (T, error) {
	var dto T
	err := json.NewDecoder(body).Decode(&dto)
	if err != nil {
		return dto, err
	}

	err = validate.Struct(dto)
	if err != nil {
		return dto, err
	}

	return dto, nil
}

func (handler *PeerMessenger) CollectResolution(c *gin.Context) {
	dto, err := getTypedRequestBody[models.ResolutionRequest](c.Request.Body, handler.validate)
	if err != nil {
		handler.logger.Error(err.Error())
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	handler.metrics.StreamResolution.WithLabelValues(dto.RoomName).Set(float64(dto.Height))
}
