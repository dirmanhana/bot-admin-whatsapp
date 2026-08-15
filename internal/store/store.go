package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register driver "pgx" untuk database/sql
	_ "modernc.org/sqlite"             // register driver "sqlite" (murni Go)
)

type Store struct {
	db      *sql.DB
	dialect string // "postgres" | "sqlite"
}

func New(db *sql.DB, dialect string) *Store {
	return &Store{db: db, dialect: dialect}
}

// q mengembalikan SQL sesuai dialect. SQLite tidak mengenal placeholder $N,
// jadi diubah menjadi ?. Postgres (pgx) memakai $N secara native.
func (s *Store) q(sql string) string {
	if s.dialect != "sqlite" {
		return sql
	}
	return toSQLitePlaceholders(sql)
}

func toSQLitePlaceholders(sql string) string {
	var b strings.Builder
	b.Grow(len(sql) + 8)
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if c == '$' && i+1 < len(sql) && sql[i+1] >= '1' && sql[i+1] <= '9' {
			b.WriteByte('?')
			i++
			for i+1 < len(sql) && sql[i+1] >= '0' && sql[i+1] <= '9' {
				i++
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func nullableTimePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	t := nt.Time
	return &t
}

// ---------- Customers ----------

func (s *Store) GetOrCreateCustomer(ctx context.Context, phone, jid, name string) (*Customer, error) {
	var c Customer
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO customers (phone, jid, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (phone) DO UPDATE SET jid = EXCLUDED.jid,
			name = CASE WHEN customers.name = '' THEN EXCLUDED.name ELSE customers.name END,
			updated_at = $4
		RETURNING id, phone, jid, name, notes, status, created_at, updated_at`),
		phone, jid, name, time.Now()).Scan(&c.ID, &c.Phone, &c.JID, &c.Name, &c.Notes, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetCustomerByPhone(ctx context.Context, phone string) (*Customer, error) {
	var c Customer
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, phone, jid, name, notes, status, created_at, updated_at FROM customers WHERE phone = $1`), phone).
		Scan(&c.ID, &c.Phone, &c.JID, &c.Name, &c.Notes, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetCustomer(ctx context.Context, id int64) (*Customer, error) {
	var c Customer
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, phone, jid, name, notes, status, created_at, updated_at FROM customers WHERE id = $1`), id).
		Scan(&c.ID, &c.Phone, &c.JID, &c.Name, &c.Notes, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ListCustomers(ctx context.Context, search string) ([]Customer, error) {
	q := `SELECT id, phone, jid, name, notes, status, created_at, updated_at FROM customers`
	args := []any{}
	if search != "" {
		// LIKE (bukan ILIKE) agar jalan di Postgres maupun SQLite.
		q += ` WHERE LOWER(phone) LIKE LOWER($1) OR LOWER(name) LIKE LOWER($1) OR LOWER(notes) LIKE LOWER($1)`
		args = append(args, "%"+search+"%")
	}
	q += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.Phone, &c.JID, &c.Name, &c.Notes, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCustomerNotes(ctx context.Context, id int64, notes string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE customers SET notes = $1, updated_at = $2 WHERE id = $3`), notes, time.Now(), id)
	return err
}

func (s *Store) SetCustomerStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE customers SET status = $1, updated_at = $2 WHERE id = $3`), status, time.Now(), id)
	return err
}

// ---------- Products ----------

func (s *Store) ListProducts(ctx context.Context, activeOnly bool) ([]Product, error) {
	q := `SELECT id, name, description, price, image_path, stock, is_active, created_at, updated_at FROM products`
	if activeOnly {
		q += ` WHERE is_active = true`
	}
	q += ` ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, s.q(q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.ImagePath, &p.Stock, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProduct(ctx context.Context, id int64) (*Product, error) {
	var p Product
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, name, description, price, image_path, stock, is_active, created_at, updated_at FROM products WHERE id = $1`), id).
		Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.ImagePath, &p.Stock, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateProduct(ctx context.Context, p *Product) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO products (name, description, price, image_path, stock, is_active)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`),
		p.Name, p.Description, p.Price, p.ImagePath, p.Stock, p.IsActive).Scan(&id)
	return id, err
}

func (s *Store) UpdateProduct(ctx context.Context, p *Product) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		UPDATE products SET name=$1, description=$2, price=$3, image_path=$4, stock=$5, is_active=$6, updated_at=$7
		WHERE id=$8`),
		p.Name, p.Description, p.Price, p.ImagePath, p.Stock, p.IsActive, time.Now(), p.ID)
	return err
}

func (s *Store) DeleteProduct(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM products WHERE id = $1`), id)
	return err
}

// ---------- Orders ----------

func (s *Store) CreateOrder(ctx context.Context, customerID int64, address string, items []OrderItem) (*Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Nomor urut order: counter tabel order_seq (kompatibel PG & SQLite,
	// menggantikan nextval sequence Postgres).
	var seq int64
	if err := tx.QueryRowContext(ctx, s.q(`
		INSERT INTO order_seq (id, val) VALUES (1, 1)
		ON CONFLICT (id) DO UPDATE SET val = order_seq.val + 1
		RETURNING val`)).Scan(&seq); err != nil {
		return nil, err
	}
	orderNumber := fmt.Sprintf("INV-%s-%04d", time.Now().Format("20060102"), seq)

	var total int64
	for _, it := range items {
		total += it.Price * int64(it.Qty)
	}

	var o Order
	err = tx.QueryRowContext(ctx, s.q(`
		INSERT INTO orders (order_number, customer_id, address, total)
		VALUES ($1, $2, $3, $4)
		RETURNING id, order_number, customer_id, status, total, address, note, created_at, updated_at`),
		orderNumber, customerID, address, total).
		Scan(&o.ID, &o.OrderNumber, &o.CustomerID, &o.Status, &o.Total, &o.Address, &o.Note, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, s.q(`
			INSERT INTO order_items (order_id, product_id, product_name, price, qty)
			VALUES ($1, $2, $3, $4, $5)`),
			o.ID, it.ProductID, it.ProductName, it.Price, it.Qty); err != nil {
			return nil, err
		}
		o.Items = append(o.Items, it)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) ListOrders(ctx context.Context, status string, limit int) ([]Order, error) {
	q := `
		SELECT o.id, o.order_number, o.customer_id, o.status, o.total, o.address, o.note, o.created_at, o.updated_at,
		       c.phone, c.name, c.status AS customer_status
		FROM orders o JOIN customers c ON c.id = o.customer_id`
	args := []any{}
	if status != "" {
		q += ` WHERE o.status = $1`
		args = append(args, status)
	}
	if limit <= 0 {
		limit = 200
	}
	q += fmt.Sprintf(` ORDER BY o.created_at DESC LIMIT %d`, limit)

	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		var c Customer
		if err := rows.Scan(&o.ID, &o.OrderNumber, &o.CustomerID, &o.Status, &o.Total, &o.Address, &o.Note,
			&o.CreatedAt, &o.UpdatedAt, &c.Phone, &c.Name, &c.Status); err != nil {
			return nil, err
		}
		o.Customer = &c
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, err := s.getOrderItems(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Items = items
	}
	return out, nil
}

func (s *Store) GetOrder(ctx context.Context, id int64) (*Order, error) {
	var o Order
	var c Customer
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT o.id, o.order_number, o.customer_id, o.status, o.total, o.address, o.note, o.created_at, o.updated_at,
		       c.phone, c.name, c.status
		FROM orders o JOIN customers c ON c.id = o.customer_id WHERE o.id = $1`), id).
		Scan(&o.ID, &o.OrderNumber, &o.CustomerID, &o.Status, &o.Total, &o.Address, &o.Note,
			&o.CreatedAt, &o.UpdatedAt, &c.Phone, &c.Name, &c.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.Customer = &c
	items, err := s.getOrderItems(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Items = items
	return &o, nil
}

func (s *Store) getOrderItems(ctx context.Context, orderID int64) ([]OrderItem, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT id, order_id, product_id, product_name, price, qty FROM order_items WHERE order_id = $1`), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrderItem
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.ProductName, &it.Price, &it.Qty); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) UpdateOrderStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE orders SET status = $1, updated_at = $2 WHERE id = $3`), status, time.Now(), id)
	return err
}

func (s *Store) DashboardStats(ctx context.Context) (*DashboardStats, error) {
	var st DashboardStats
	startOfDay := time.Now().Truncate(24 * time.Hour)
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT
			(SELECT COUNT(*) FROM orders WHERE created_at >= $1),
			(SELECT COALESCE(SUM(total),0) FROM orders WHERE created_at >= $2 AND status != 'batal'),
			(SELECT COUNT(*) FROM customers),
			(SELECT COUNT(*) FROM orders WHERE status = 'baru')`),
		startOfDay, startOfDay).Scan(&st.OrdersToday, &st.RevenueToday, &st.TotalCustomers, &st.PendingOrders)
	return &st, err
}

// ---------- Chat messages ----------

func (s *Store) SaveChatMessage(ctx context.Context, m *ChatMessage) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO chat_messages (customer_id, direction, message_type, body, media_url, wa_message_id)
		VALUES ($1, $2, $3, $4, $5, $6)`),
		m.CustomerID, m.Direction, m.MessageType, m.Body, m.MediaURL, m.WAMessageID)
	return err
}

func (s *Store) ListChatMessages(ctx context.Context, customerID int64, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT cm.id, cm.customer_id, cm.direction, cm.message_type, cm.body, cm.media_url, cm.wa_message_id, cm.created_at,
		       c.name, c.phone
		FROM chat_messages cm JOIN customers c ON c.id = cm.customer_id
		WHERE cm.customer_id = $1 ORDER BY cm.created_at ASC LIMIT $2`), customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.CustomerID, &m.Direction, &m.MessageType, &m.Body, &m.MediaURL, &m.WAMessageID,
			&m.CreatedAt, &m.CustomerName, &m.Phone); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) RecentConversations(ctx context.Context) ([]Conversation, error) {
	// DISTINCT ON (khusus Postgres) diganti subquery MAX(id) agar kompatibel
	// dengan SQLite juga.
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT cm.customer_id, c.phone, c.name, cm.body, cm.direction, cm.created_at
		FROM chat_messages cm JOIN customers c ON c.id = cm.customer_id
		WHERE cm.id IN (SELECT MAX(id) FROM chat_messages GROUP BY customer_id)
		ORDER BY cm.created_at DESC LIMIT 200`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var cv Conversation
		if err := rows.Scan(&cv.CustomerID, &cv.Phone, &cv.Name, &cv.LastBody, &cv.LastDirection, &cv.LastAt); err != nil {
			return nil, err
		}
		out = append(out, cv)
	}
	return out, rows.Err()
}

// ---------- Quick replies ----------

func (s *Store) ListQuickReplies(ctx context.Context) ([]QuickReply, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, keyword, reply, is_active, created_at FROM quick_replies ORDER BY keyword ASC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuickReply
	for rows.Next() {
		var q QuickReply
		if err := rows.Scan(&q.ID, &q.Keyword, &q.Reply, &q.IsActive, &q.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) GetQuickReplyByKeyword(ctx context.Context, keyword string) (*QuickReply, error) {
	var q QuickReply
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, keyword, reply, is_active, created_at FROM quick_replies
		WHERE keyword = $1 AND is_active = true`), strings.ToLower(strings.TrimSpace(keyword))).
		Scan(&q.ID, &q.Keyword, &q.Reply, &q.IsActive, &q.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

func (s *Store) CreateQuickReply(ctx context.Context, q *QuickReply) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO quick_replies (keyword, reply, is_active) VALUES ($1, $2, $3) RETURNING id`),
		strings.ToLower(strings.TrimSpace(q.Keyword)), q.Reply, q.IsActive).Scan(&id)
	return id, err
}

func (s *Store) UpdateQuickReply(ctx context.Context, q *QuickReply) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		UPDATE quick_replies SET keyword = $1, reply = $2, is_active = $3 WHERE id = $4`),
		strings.ToLower(strings.TrimSpace(q.Keyword)), q.Reply, q.IsActive, q.ID)
	return err
}

func (s *Store) DeleteQuickReply(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM quick_replies WHERE id = $1`), id)
	return err
}

// ---------- Broadcasts ----------

func (s *Store) CreateBroadcast(ctx context.Context, b *Broadcast) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO broadcasts (message, segment, total_targets) VALUES ($1, $2, $3) RETURNING id`),
		b.Message, b.Segment, b.TotalTargets).Scan(&id)
	return id, err
}

func (s *Store) ListBroadcasts(ctx context.Context) ([]Broadcast, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, message, segment, total_targets, sent, failed, status, created_at, finished_at
		FROM broadcasts ORDER BY created_at DESC LIMIT 100`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Broadcast
	for rows.Next() {
		var b Broadcast
		var fin sql.NullTime
		if err := rows.Scan(&b.ID, &b.Message, &b.Segment, &b.TotalTargets, &b.Sent, &b.Failed, &b.Status,
			&b.CreatedAt, &fin); err != nil {
			return nil, err
		}
		b.FinishedAt = nullableTimePtr(fin)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBroadcast(ctx context.Context, id int64) (*Broadcast, error) {
	var b Broadcast
	var fin sql.NullTime
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, message, segment, total_targets, sent, failed, status, created_at, finished_at
		FROM broadcasts WHERE id = $1`), id).
		Scan(&b.ID, &b.Message, &b.Segment, &b.TotalTargets, &b.Sent, &b.Failed, &b.Status, &b.CreatedAt, &fin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.FinishedAt = nullableTimePtr(fin)
	return &b, nil
}

func (s *Store) SetBroadcastRunning(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE broadcasts SET status = 'running' WHERE id = $1 AND status = 'pending'`), id)
	return err
}

func (s *Store) BroadcastProgress(ctx context.Context, id int64, sent, failed int) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE broadcasts SET sent = $2, failed = $3 WHERE id = $1`), id, sent, failed)
	return err
}

func (s *Store) FinishBroadcast(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE broadcasts SET status = $2, finished_at = $3 WHERE id = $1`), id, status, time.Now())
	return err
}

// ---------- Order sessions ----------

func (s *Store) GetOrderSession(ctx context.Context, customerID int64) (*OrderSession, error) {
	var os OrderSession
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT customer_id, state, product_id, qty, address, updated_at FROM order_sessions WHERE customer_id = $1`), customerID).
		Scan(&os.CustomerID, &os.State, &os.ProductID, &os.Qty, &os.Address, &os.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &os, nil
}

func (s *Store) UpsertOrderSession(ctx context.Context, os *OrderSession) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO order_sessions (customer_id, state, product_id, qty, address, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (customer_id) DO UPDATE SET
			state = EXCLUDED.state, product_id = EXCLUDED.product_id, qty = EXCLUDED.qty,
			address = EXCLUDED.address, updated_at = EXCLUDED.updated_at`),
		os.CustomerID, os.State, os.ProductID, os.Qty, os.Address, time.Now())
	return err
}

func (s *Store) DeleteOrderSession(ctx context.Context, customerID int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM order_sessions WHERE customer_id = $1`), customerID)
	return err
}

// ---------- WA accounts ----------

func (s *Store) ListWAAccounts(ctx context.Context) ([]WAAccount, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, username, token, token_expires_at, device_id, is_active, created_at, updated_at
		FROM wa_accounts ORDER BY id ASC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WAAccount
	for rows.Next() {
		var a WAAccount
		var exp sql.NullTime
		if err := rows.Scan(&a.ID, &a.Username, &a.Token, &exp, &a.DeviceID, &a.IsActive,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.TokenExpiresAt = nullableTimePtr(exp)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetWAAccountByUsername(ctx context.Context, username string) (*WAAccount, error) {
	var a WAAccount
	var exp sql.NullTime
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, username, password, token, token_expires_at, device_id, is_active, created_at, updated_at
		FROM wa_accounts WHERE username = $1`), username).
		Scan(&a.ID, &a.Username, &a.Password, &a.Token, &exp, &a.DeviceID, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.TokenExpiresAt = nullableTimePtr(exp)
	return &a, nil
}

func (s *Store) GetActiveWAAccount(ctx context.Context) (*WAAccount, error) {
	var a WAAccount
	var exp sql.NullTime
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, username, password, token, token_expires_at, device_id, is_active, created_at, updated_at
		FROM wa_accounts WHERE is_active = true ORDER BY id ASC LIMIT 1`)).
		Scan(&a.ID, &a.Username, &a.Password, &a.Token, &exp, &a.DeviceID, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.TokenExpiresAt = nullableTimePtr(exp)
	return &a, nil
}

func (s *Store) UpsertWAAccount(ctx context.Context, username, password, deviceID string, isActive bool) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO wa_accounts (username, password, device_id, is_active)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (username) DO UPDATE SET password = EXCLUDED.password,
			device_id = EXCLUDED.device_id, is_active = EXCLUDED.is_active, updated_at = $5
		RETURNING id`),
		username, password, deviceID, isActive, time.Now()).Scan(&id)
	return id, err
}

func (s *Store) SetWAAccountActive(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE wa_accounts SET is_active = false`)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE wa_accounts SET is_active = true WHERE id = $1`), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateWAToken(ctx context.Context, id int64, token string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE wa_accounts SET token = $1, token_expires_at = $2, updated_at = $3 WHERE id = $4`),
		token, expiresAt, time.Now(), id)
	return err
}

// ---------- Settings ----------

func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT key, value FROM settings`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`), key, value)
	return err
}

// ---------- Knowledge base ----------

type KnowledgeDoc struct {
	ID         int64     `json:"id"`
	Filename   string    `json:"filename"`
	FileType   string    `json:"file_type"`
	SizeBytes  int64     `json:"size_bytes"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) ListKnowledgeDocs(ctx context.Context) ([]KnowledgeDoc, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, filename, file_type, size_bytes, chunk_count, created_at
		FROM knowledge_docs ORDER BY created_at DESC`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnowledgeDoc
	for rows.Next() {
		var d KnowledgeDoc
		if err := rows.Scan(&d.ID, &d.Filename, &d.FileType, &d.SizeBytes, &d.ChunkCount, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) AddKnowledgeDoc(ctx context.Context, doc *KnowledgeDoc, data []byte) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO knowledge_docs (filename, file_type, size_bytes, chunk_count, data)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`),
		doc.Filename, doc.FileType, doc.SizeBytes, doc.ChunkCount, data).Scan(&id)
	return id, err
}

func (s *Store) AddKnowledgeChunks(ctx context.Context, docID int64, chunks []string) error {
	if len(chunks) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, ch := range chunks {
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO knowledge_chunks (doc_id, content) VALUES ($1, $2)`), docID, ch); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE knowledge_docs SET chunk_count = $2 WHERE id = $1`), docID, len(chunks)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteKnowledgeDoc(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM knowledge_docs WHERE id = $1`), id)
	return err
}

// SearchKnowledgeChunks returns the most relevant chunks for a question.
// Ambil kandidat via full-text search Postgres (atau LIKE sebagai cadangan),
// lalu urutkan ulang di Go berdasarkan jumlah kata pertanyaan yang muncul.
func (s *Store) SearchKnowledgeChunks(ctx context.Context, query string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	words := queryWords(q)
	if len(words) == 0 {
		return nil, nil
	}

	candidates := map[int64]string{}

	// 1) Full-text search — khusus Postgres (to_tsvector). SQLite memakai LIKE.
	if s.dialect != "sqlite" {
		rows, err := s.db.QueryContext(ctx, s.q(`
			SELECT id, content FROM knowledge_chunks
			WHERE to_tsvector('simple', content) @@ plainto_tsquery('simple', $1)
			ORDER BY ts_rank(to_tsvector('simple', content), plainto_tsquery('simple', $1)) DESC
			LIMIT 20`), q)
		if err == nil {
			for rows.Next() {
				var id int64
				var c string
				if err := rows.Scan(&id, &c); err != nil {
					rows.Close()
					return nil, err
				}
				candidates[id] = c
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
	}

	// 2) Cadangan: LIKE bila full-text tidak menemukan cukup (berlaku untuk
	//    Postgres maupun SQLite).
	if len(candidates) < limit {
		where := ""
		args := []any{}
		for i, w := range words {
			if i > 0 {
				where += " OR "
			}
			where += fmt.Sprintf("LOWER(content) LIKE LOWER($%d)", i+1)
			args = append(args, "%"+w+"%")
		}
		rows2, err := s.db.QueryContext(ctx, s.q(fmt.Sprintf(`
			SELECT id, content FROM knowledge_chunks WHERE %s ORDER BY id DESC LIMIT 20`,
			where)), args...)
		if err == nil {
			for rows2.Next() {
				var id int64
				var c string
				if err := rows2.Scan(&id, &c); err != nil {
					rows2.Close()
					return nil, err
				}
				if _, ok := candidates[id]; !ok {
					candidates[id] = c
				}
			}
			rows2.Close()
		}
	}

	// 3) Ranking: jumlah kata pertanyaan yang muncul di chunk
	type scored struct {
		content string
		score   int
	}
	all := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		lc := strings.ToLower(c)
		n := 0
		for _, w := range words {
			if strings.Contains(lc, w) {
				n++
			}
		}
		all = append(all, scored{content: c, score: n})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return len(all[i].content) < len(all[j].content)
	})

	out := make([]string, 0, limit)
	for _, s := range all {
		out = append(out, s.content)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// queryWords memecah pertanyaan menjadi kata penting (>= 3 huruf).
func queryWords(q string) []string {
	var words []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool {
		return r <= ' ' || r == ',' || r == '.' || r == ';' || r == ':' || r == '?' || r == '!' || r == '"' || r == '\''
	}) {
		w = strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]{}-"))
		if len([]rune(w)) >= 3 {
			words = append(words, w)
		}
	}
	return words
}
