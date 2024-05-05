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
	channelThreshold = 40
	maxMsgRPS        = 100
)

type Room struct {
	userMessages map[string]chan models.ChannelEntity
	mux          *sync.RWMutex
	log          *zap.Logger
	sendLimiter  *rate.Limiter
}

func NewRoom(log *zap.Logger) *Room {
	return &Room{
		userMessages: make(map[string]chan models.ChannelEntity),
		mux:          &sync.RWMutex{},
		log:          log,
		sendLimiter:  rate.NewLimiter(rate.Limit(maxMsgRPS), 2*maxMsgRPS),
	}
}

func (r *Room) publish(entity models.ChannelEntity) {
	r.log.Info("gonna send to message to users", zap.Int("users number", len(r.userMessages)-1))

	for userID, toChannel := range r.userMessages {
		if userID != entity.UserID {
			toChannel <- entity
		}
	}
}

func (r *Room) AddUser(userID string) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	if _, ok := r.userMessages[userID]; ok {
		return ErrUserAlreadyInRoom
	}

	r.userMessages[userID] = make(chan models.ChannelEntity, 100)

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

	if _, ok := r.userMessages[userID]; !ok {
		return ErrUserNotInRoom
	}

	ch := r.userMessages[userID]
	delete(r.userMessages, userID)
	close(ch)

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

	userCh, ok := r.userMessages[userID]
	if !ok {
		return nil, ErrUserNotInRoom
	}

	return userCh, nil
}

func (r *Room) GetUserEventsSlice(userID string) ([]models.ChannelEntity, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	userCh, ok := r.userMessages[userID]
	if !ok {
		return nil, ErrUserNotInRoom
	}

	entities := make([]models.ChannelEntity, 0, 2*len(userCh))
	for len(userCh) > 0 {
		entity, ok := <-userCh
		if !ok {
			break
		}

		entities = append(entities, entity)
	}

	return entities, nil
}

func (r *Room) SendToUser(ctx context.Context, destUserID string, data map[string]any) error {
	err := r.sendLimiter.Wait(ctx)
	if err != nil {
		r.log.Warn("send limiter cancelled", zap.String("reason", err.Error()))
		return err
	}

	r.mux.RLock()
	defer r.mux.RUnlock()

	ch, ok := r.userMessages[destUserID]
	if !ok {
		return ErrUserNotInRoom
	}

	ch <- models.ChannelEntity{
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

	clearedCnt := 0
	for _, ch := range r.userMessages {
		if len(ch) > channelThreshold {
			clearedCnt++
			close(ch)
		}
	}

	r.log.Info("cleared room", zap.Int("cleared number", clearedCnt))
}
