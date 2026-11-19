package service

import "time"

type GuildState struct {
	Character string    `json:"character"`
	Guild     string    `json:"guild"`
	State     string    `json:"state"`
	Have      int       `json:"have,omitempty"`
	Need      int       `json:"need,omitempty"`
	LiveSince time.Time `json:"liveSince,omitzero"`
}

type QueueLine struct {
	Character string    `json:"character"`
	State     string    `json:"state"`
	Ahead     int       `json:"ahead,omitempty"`
	DoneAt    time.Time `json:"doneAt,omitzero"`
}

type Own struct {
	Counted    int      `json:"counted"`
	Awaiting   int      `json:"awaiting"`
	NotCounted int      `json:"notCounted"`
	Reasons    []string `json:"reasons,omitempty"`
}
