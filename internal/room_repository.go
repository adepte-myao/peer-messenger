package internal

import (
	"errors"
	"sync"

	"go.uber.org/zap"
)

var (
	ErrRoomNotExist     = errors.New("room does not exist")
	ErrRoomAlreadyExist = errors.New("room already exists")
)

type RoomRepository struct {
	rooms map[string]*Room
	mut   *sync.RWMutex
	log   *zap.Logger
}

func NewRoomRepository(log *zap.Logger) *RoomRepository {
	return &RoomRepository{
		rooms: make(map[string]*Room),
		mut:   &sync.RWMutex{},
		log:   log,
	}
}

// Clean is a blocking call that makes each room drop disconnected users then removes all empty rooms
func (repo *RoomRepository) Clean() {
	repo.mut.Lock()
	defer repo.mut.Unlock()

	toRemove := make([]string, 0)
	for roomID, room := range repo.rooms {
		room.RemoveDisconnected()

		if room.IsEmpty() {
			toRemove = append(toRemove, roomID)
		}
	}

	for _, roomID := range toRemove {
		delete(repo.rooms, roomID)
	}

	if len(toRemove) > 0 {
		repo.log.Info(
			"removed some rooms",
			zap.Int("removed count", len(toRemove)),
			zap.Any("removed", toRemove),
		)
	}
}

func (repo *RoomRepository) Get(roomName string) (*Room, error) {
	repo.mut.RLock()
	defer repo.mut.RUnlock()

	room, ok := repo.rooms[roomName]
	if !ok {
		return nil, ErrRoomNotExist
	}

	return room, nil
}

func (repo *RoomRepository) Exist(roomName string) bool {
	repo.mut.RLock()
	defer repo.mut.RUnlock()

	_, ok := repo.rooms[roomName]
	return ok
}

func (repo *RoomRepository) AddRoom(roomName string) (*Room, error) {
	repo.mut.Lock()
	defer repo.mut.Unlock()

	if _, ok := repo.rooms[roomName]; ok {
		return nil, ErrRoomAlreadyExist
	}

	roomLog := repo.log.With(zap.String("room name", roomName))
	room := NewRoom(roomLog)
	repo.rooms[roomName] = room

	return room, nil
}
