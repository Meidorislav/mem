package storage

import "time"

type MemorySource string

const (
	SourceSave     MemorySource = "save"
	SourceRemember MemorySource = "remember"
	SourceWatch    MemorySource = "watch"
)

type Memory struct {
	ID          int64
	Title       string
	Source      MemorySource
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Commands    []Command
	Tags        []string
}

type Command struct {
	ID        int64
	MemoryID  int64
	Position  int
	Command   string
	Output    *string
	CreatedAt time.Time
}

type Tag struct {
	ID   int64
	Name string
}
