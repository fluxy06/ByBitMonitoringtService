package help

import (
	"time"
)

type User struct {
	ID         uint  `gorm:"primaryKey"`
	TelegramID int64 `gorm:"unique"`
	CreatedAt  time.Time
}

type UserTicker struct {
	ID         uint `gorm:"primaryKey"`
	UserID     uint
	NameTicker string `gorm:"size:20"`
	CreatedAt  time.Time
}
