package store

import "time"

type Customer struct {
	ID        int64     `json:"id"`
	Phone     string    `json:"phone"`
	JID       string    `json:"jid"`
	Name      string    `json:"name"`
	Notes     string    `json:"notes"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Product struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Price        int64     `json:"price"`
	ImagePath    string    `json:"image_path"`
	Stock        int       `json:"stock"`
	IsActive     bool      `json:"is_active"`
	PurchaseLink string    `json:"purchase_link"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type OrderItem struct {
	ID          int64  `json:"id"`
	OrderID     int64  `json:"order_id"`
	ProductID   int64  `json:"product_id"`
	ProductName string `json:"product_name"`
	Price       int64  `json:"price"`
	Qty         int    `json:"qty"`
}

type Order struct {
	ID           int64       `json:"id"`
	OrderNumber  string      `json:"order_number"`
	CustomerID   int64       `json:"customer_id"`
	Status       string      `json:"status"`
	Total        int64       `json:"total"`
	Address      string      `json:"address"`
	DeliveryType string      `json:"delivery_type"`
	DeliveryFee  int64       `json:"delivery_fee"`
	Note         string      `json:"note"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	Customer     *Customer   `json:"customer,omitempty"`
	Items        []OrderItem `json:"items,omitempty"`
}

type ChatMessage struct {
	ID           int64     `json:"id"`
	CustomerID   int64     `json:"customer_id"`
	Direction    string    `json:"direction"`
	MessageType  string    `json:"message_type"`
	Body         string    `json:"body"`
	MediaURL     string    `json:"media_url"`
	WAMessageID  string    `json:"wa_message_id"`
	CreatedAt    time.Time `json:"created_at"`
	CustomerName string    `json:"customer_name,omitempty"`
	Phone        string    `json:"phone,omitempty"`
}

type QuickReply struct {
	ID        int64     `json:"id"`
	Keyword   string    `json:"keyword"`
	Reply     string    `json:"reply"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type Broadcast struct {
	ID           int64      `json:"id"`
	Message      string     `json:"message"`
	Segment      string     `json:"segment"`
	TotalTargets int        `json:"total_targets"`
	Sent         int        `json:"sent"`
	Failed       int        `json:"failed"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

type OrderSession struct {
	CustomerID   int64       `json:"customer_id"`
	State        string      `json:"state"`
	ProductID    int64       `json:"product_id"`
	Qty          int         `json:"qty"`
	Address      string      `json:"address"`
	DeliveryType string      `json:"delivery_type"`
	Items        []OrderItem `json:"items"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type WAAccount struct {
	ID             int64      `json:"id"`
	Username       string     `json:"username"`
	Password       string     `json:"-"`
	Token          string     `json:"-"`
	TokenExpiresAt *time.Time `json:"token_expires_at"`
	DeviceID       string     `json:"device_id"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type DashboardStats struct {
	OrdersToday    int64 `json:"orders_today"`
	RevenueToday   int64 `json:"revenue_today"`
	TotalCustomers int64 `json:"total_customers"`
	PendingOrders  int64 `json:"pending_orders"`
}

type Conversation struct {
	CustomerID    int64     `json:"customer_id"`
	Phone         string    `json:"phone"`
	Name          string    `json:"name"`
	LastBody      string    `json:"last_body"`
	LastDirection string    `json:"last_direction"`
	LastAt        time.Time `json:"last_at"`
}
