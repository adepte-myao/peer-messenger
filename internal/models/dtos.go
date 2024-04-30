package models

import (
	"time"
)

type LoginRequest struct {
	UserID   string `json:"userID" validate:"required"`
	PassHash string `json:"passHash"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type ChannelRequest struct {
	ChannelName string `json:"channelName" validate:"required"`
}

type ChannelEntity struct {
	Time       time.Time      `json:"time"`
	ActionType ActionType     `json:"actionType"`
	UserID     string         `json:"userID"`
	Data       map[string]any `json:"data"`
}

type ActionType string

const (
	UserJoined ActionType = "user joined"
	UserLeft   ActionType = "user left"
	Message    ActionType = "message"
)

type SendToPeerRequest struct {
	ChannelName       string         `json:"channelName"`
	DestinationUserID string         `json:"destinationUserID"`
	Message           map[string]any `json:"message"`
}
