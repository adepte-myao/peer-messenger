package internal

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"peer-messenger/internal/models"
)

var (
	ErrUserAlreadyInRoom = errors.New("user is already in room")
	ErrUserNotInRoom     = errors.New("user is not in room")
)

const (
	msgCountThreshold     = 40
	maxMsgRPS             = 100
	maxInactivityDuration = time.Minute
)

type Room struct {
	userInfos   map[string]*userInfo
	mux         *sync.RWMutex
	log         *zap.Logger
	sendLimiter *rate.Limiter
}

type userInfo struct {
	entities       chan models.ChannelEntity
	lastActionTime time.Time
}

func NewRoom(log *zap.Logger) *Room {
	return &Room{
		userInfos:   make(map[string]*userInfo),
		mux:         &sync.RWMutex{},
		log:         log,
		sendLimiter: rate.NewLimiter(rate.Limit(maxMsgRPS), 2*maxMsgRPS),
	}
}

func (r *Room) publish(entity models.ChannelEntity) {
	r.log.Info("gonna send to message to users", zap.Int("users number", len(r.userInfos)-1))

	for userID, info := range r.userInfos {
		if userID != entity.UserID {
			info.entities <- entity
		}
	}
}

func (r *Room) AddUser(userID string) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	if _, ok := r.userInfos[userID]; ok {
		return ErrUserAlreadyInRoom
	}

	r.userInfos[userID] = &userInfo{
		entities:       make(chan models.ChannelEntity, 100),
		lastActionTime: time.Now(),
	}

	r.publish(models.ChannelEntity{
		Time:       time.Now(),
		ActionType: models.UserJoined,
		UserID:     userID,
		Data:       nil,
	})

	return nil
}

func (r *Room) RemoveUser(userID string) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	if _, ok := r.userInfos[userID]; !ok {
		return ErrUserNotInRoom
	}

	info := r.userInfos[userID]
	delete(r.userInfos, userID)
	close(info.entities)

	r.publish(models.ChannelEntity{
		Time:       time.Now(),
		ActionType: models.UserLeft,
		UserID:     userID,
		Data:       nil,
	})

	return nil
}

func (r *Room) GetUserEventsChan(userID string) (<-chan models.ChannelEntity, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	info, ok := r.userInfos[userID]
	if !ok {
		return nil, ErrUserNotInRoom
	}

	info.lastActionTime = time.Now()

	return info.entities, nil
}

func (r *Room) GetUserEventsSlice(userID string) ([]models.ChannelEntity, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	info, ok := r.userInfos[userID]
	if !ok {
		return nil, ErrUserNotInRoom
	}

	userCh := info.entities

	entities := make([]models.ChannelEntity, 0, 2*len(userCh))
	for len(userCh) > 0 {
		entity, ok := <-userCh
		if !ok {
			break
		}

		entities = append(entities, entity)
	}

	info.lastActionTime = time.Now()

	return entities, nil
}

func (r *Room) SendToUser(ctx context.Context, srcUserID, destUserID string, data map[string]any) error {
	err := r.sendLimiter.Wait(ctx)
	if err != nil {
		r.log.Warn("send limiter cancelled", zap.String("reason", err.Error()))
		return err
	}

	r.mux.RLock()
	defer r.mux.RUnlock()

	srcInfo, ok := r.userInfos[srcUserID]
	if !ok {
		return ErrUserNotInRoom
	}

	srcInfo.lastActionTime = time.Now()

	destInfo, ok := r.userInfos[destUserID]
	if !ok {
		return ErrUserNotInRoom
	}

	destInfo.entities <- models.ChannelEntity{
		Time:       time.Now(),
		ActionType: models.Message,
		UserID:     destUserID,
		Data:       data,
	}

	return nil
}

func (r *Room) RemoveDisconnected() {
	r.log.Info("clearing room")

	r.mux.Lock()
	defer r.mux.Unlock()

	toDelete := make([]string, 0)
	for userID, info := range r.userInfos {
		if len(info.entities) > msgCountThreshold || time.Since(info.lastActionTime) > maxInactivityDuration {
			toDelete = append(toDelete, userID)
		}
	}

	for _, userID := range toDelete {
		info := r.userInfos[userID]
		delete(r.userInfos, userID)
		close(info.entities)

		r.publish(models.ChannelEntity{
			Time:       time.Now(),
			ActionType: models.UserLeft,
			UserID:     userID,
			Data:       nil,
		})
	}

	r.log.Info("cleared room", zap.Int("cleared number", len(toDelete)), zap.Any("deleted", toDelete))
}

func (r *Room) IsEmpty() bool {
	return len(r.userInfos) == 0
}
