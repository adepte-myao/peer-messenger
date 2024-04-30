package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"golang.org/x/sync/errgroup"

	"peer-messenger/internal/loggers"
	"peer-messenger/internal/models"
)

type channelKey struct {
	ChannelName string
}

type channel map[string]chan models.ChannelEntity

func (ch *channel) Publish(entity models.ChannelEntity) {
	fmt.Printf("gonna send to %d users\n", len(*ch))
	for _, toChannel := range *ch {
		toChannel <- entity
	}
}

type PeerMessenger struct {
	logger            loggers.Logger
	salt              []byte
	validate          *validator.Validate
	users             map[string]struct{}
	channelGroups     map[channelKey]channel
	channelGroupMutex sync.RWMutex
	keysExtractor     *regexp.Regexp
}

func NewPeerMessenger(logger loggers.Logger, validate *validator.Validate) *PeerMessenger {
	salt := []byte("asasasas")

	out := &PeerMessenger{
		logger:            logger,
		salt:              salt,
		validate:          validate,
		users:             make(map[string]struct{}),
		channelGroups:     make(map[channelKey]channel),
		channelGroupMutex: sync.RWMutex{},
		keysExtractor:     regexp.MustCompile(`[^_]+`),
	}

	g := new(errgroup.Group)

	g.Go(func() error {
		for {
			time.Sleep(10 * time.Second)

			out.channelGroupMutex.RLock()
			chans := make([]chan models.ChannelEntity, 0)
			for _, chanGroup := range out.channelGroups {
				for _, ch := range chanGroup {
					chans = append(chans, ch)
				}
			}
			out.channelGroupMutex.RUnlock()

			for _, ch := range chans {
				if len(ch) > 10 {
					close(ch)
				}
			}
		}
	})

	return out
}

func (handler *PeerMessenger) Register(c *gin.Context) {
	c.AbortWithStatus(http.StatusNotImplemented)
}

func (handler *PeerMessenger) Login(c *gin.Context) {
	var dto models.LoginRequest

	err := json.NewDecoder(c.Request.Body).Decode(&dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	err = handler.validate.Struct(dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	// add temporal user for now
	handler.users[dto.UserID] = struct{}{}

	token := dto.UserID + string(handler.salt)
	c.JSON(http.StatusOK, models.LoginResponse{Token: token})
}

func (handler *PeerMessenger) JoinChannel(c *gin.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		err := errors.New("header Authorization is empty")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	lastIndex := len(token) - len(handler.salt)
	userID := token[:lastIndex]

	if _, ok := handler.users[userID]; !ok {
		err := errors.New("user does not exist")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var dto models.ChannelRequest
	err := json.NewDecoder(c.Request.Body).Decode(&dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	err = handler.validate.Struct(dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	handler.channelGroupMutex.Lock()

	groupKey := channelKey{ChannelName: dto.ChannelName}
	channelGroup, ok := handler.channelGroups[groupKey]
	if !ok {
		channelGroup = make(channel)
		handler.channelGroups[groupKey] = channelGroup
	}

	channelGroup.Publish(models.ChannelEntity{
		Time:       time.Now(),
		ActionType: models.UserJoined,
		UserID:     userID,
		Data:       nil,
	})

	userChan := make(chan models.ChannelEntity, 100)
	channelGroup[userID] = userChan
	handler.channelGroups[groupKey] = channelGroup

	handler.channelGroupMutex.Unlock()

	subscriptionID := fmt.Sprintf("%s__%s", dto.ChannelName, userID)

	c.JSON(http.StatusOK, map[string]string{"subscriptionID": subscriptionID})
}

func (handler *PeerMessenger) LeaveChannel(c *gin.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		err := errors.New("header Authorization is empty")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	lastIndex := len(token) - len(handler.salt)
	userID := token[:lastIndex]

	if _, ok := handler.users[userID]; !ok {
		err := errors.New("user does not exist")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var dto models.ChannelRequest
	err := json.NewDecoder(c.Request.Body).Decode(&dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	err = handler.validate.Struct(dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	handler.channelGroupMutex.Lock()
	defer handler.channelGroupMutex.Unlock()

	groupKey := channelKey{ChannelName: dto.ChannelName}
	channelGroup, ok := handler.channelGroups[groupKey]
	if !ok {
		err = errors.New("no channel to leave")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	channelGroup.Publish(models.ChannelEntity{
		Time:       time.Now(),
		ActionType: models.UserLeft,
		UserID:     userID,
		Data:       nil,
	})

	delete(channelGroup, userID)

	c.JSON(http.StatusOK, map[string]string{"status": "OK"})
}

func (handler *PeerMessenger) Subscribe(c *gin.Context) {
	subscriptionID, ok := c.GetQuery("subscriptionID")
	if !ok || subscriptionID == "" {
		err := errors.New("subscriptionID is not provided")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	keys := handler.keysExtractor.FindAllString(subscriptionID, 2)

	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Content-Type", "text/event-stream")

	handler.channelGroupMutex.RLock()

	groupKey := channelKey{ChannelName: keys[0]}
	channelGroup, ok := handler.channelGroups[groupKey]
	if !ok {
		err := errors.New("channel does not exist")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)

		handler.channelGroupMutex.RUnlock()
		return
	}

	userChan := channelGroup[keys[1]]

	handler.channelGroupMutex.RUnlock()

	for entity := range userChan {
		c.SSEvent("message", entity)
		c.Writer.Flush() // Why wasn't it in THE GIN Framework????
	}

	fmt.Printf("leaving from %s group\n", groupKey)

	c.AbortWithStatus(http.StatusNoContent)
}

func (handler *PeerMessenger) CollectMessages(c *gin.Context) {
	subscriptionID, ok := c.GetQuery("subscriptionID")
	if !ok || subscriptionID == "" {
		err := errors.New("subscriptionID is not provided")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	keys := handler.keysExtractor.FindAllString(subscriptionID, 2)

	handler.channelGroupMutex.RLock()

	groupKey := channelKey{ChannelName: keys[0]}
	channelGroup, ok := handler.channelGroups[groupKey]
	if !ok {
		err := errors.New("channel does not exist")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)

		handler.channelGroupMutex.RUnlock()
		return
	}

	userChan := channelGroup[keys[1]]

	handler.channelGroupMutex.RUnlock()

	entities := make([]models.ChannelEntity, 0, len(userChan))
	for len(userChan) > 0 {
		entity := <-userChan

		entities = append(entities, entity)
	}

	c.JSON(http.StatusOK, map[string][]models.ChannelEntity{"entities": entities})
}

func (handler *PeerMessenger) SendToPeer(c *gin.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		err := errors.New("header Authorization is empty")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	lastIndex := len(token) - len(handler.salt)
	userID := token[:lastIndex]

	if _, ok := handler.users[userID]; !ok {
		err := errors.New("user does not exist")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var dto models.SendToPeerRequest
	err := json.NewDecoder(c.Request.Body).Decode(&dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	err = handler.validate.Struct(dto)
	if err != nil {
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	handler.channelGroupMutex.RLock()

	groupKey := channelKey{ChannelName: dto.ChannelName}
	chanGroup, ok := handler.channelGroups[groupKey]
	if !ok {
		handler.channelGroupMutex.RUnlock()

		err = errors.New("given channel does not exist")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)

		return
	}

	userChan, ok := chanGroup[dto.DestinationUserID]
	if !ok {
		handler.channelGroupMutex.RUnlock()

		err = errors.New("the is no user with this id in the channel")
		handler.logger.Error(err)
		_ = c.AbortWithError(http.StatusBadRequest, err)

		return
	}

	userChan <- models.ChannelEntity{
		Time:       time.Now(),
		ActionType: models.Message,
		UserID:     userID,
		Data:       dto.Message,
	}

	handler.channelGroupMutex.RUnlock()

	c.AbortWithStatus(http.StatusOK)
}
