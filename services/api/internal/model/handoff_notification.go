package model

import "time"

type HandoffNotification struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	ReceiptID     string     `json:"receiptId"`
	EmployeeID    string     `json:"-"`
	Kind          string     `json:"kind"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReadAt        *time.Time `json:"readAt"`
	TaskTitle     string     `gorm:"->;-:migration" json:"taskTitle"`
	FromName      string     `gorm:"->;-:migration" json:"fromName"`
	ToName        string     `gorm:"->;-:migration" json:"toName"`
	ReceiptStatus string     `gorm:"->;-:migration" json:"receiptStatus"`
}
