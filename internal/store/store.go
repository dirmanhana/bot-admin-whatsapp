package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // register driver "pgx" untuk database/sql

	"github.com/dirman/bot-admin-whatsapp/internal/cryptx"
)

type Store struct {
	db     *sql.DB
	cipher *cryptx.Cipher
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetCipher memasang enkripsi untuk kredensial (password/token gowa).
// Tanpa cipher, nilai disimpan/dibaca polos (mode lama).
func (s *Store) SetCipher(c *cryptx.Cipher) {
	s.cipher = c
}

// enc mengamankan nilai sebelum disimpan ke DB; kosong bila tanpa cipher.
func (s *Store) enc(v string) string {
	if s.cipher == nil || v == "" {
		return v
	}
	out, err := s.cipher.Encrypt(v)
	if err != nil {
		return v
	}
	return out
}

// dec membaca nilai dari DB; data lama (tanpa prefix "enc:") tetap terbaca.
func (s *Store) dec(v string) string {
	if s.cipher == nil || v == "" {
		return v
	}
	out, err := s.cipher.Decrypt(v)
	if err != nil {
		return v
	}
	return out
}

// tid mengambil ID tenant dari context; 0/belum di-set dianggap tenant 1
// (data lama milik tenant 1) agar request pra-login tidak kehilangan data.
func (s *Store) tid(ctx context.Context) int64 {
	if t := TenantID(ctx); t > 0 {
		return t
	}
	return 1
}

// q mengembalikan SQL apa adanya — aplikasi ini PostgreSQL-only, jadi tidak
// ada konversi placeholder. Dibiarkan sebagai method agar perubahan kecil
// dan semua pemanggil tidak perlu diubah.
func (s *Store) q(sql string) string {
	return sql
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
	tid := s.tid(ctx)
	var c Customer
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO customers (tenant_id, phone, jid, name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, phone) DO UPDATE SET jid = EXCLUDED.jid,
			name = CASE WHEN customers.name = '' THEN EXCLUDED.name ELSE customers.name END,
			updated_at = $5
		RETURNING id, phone, jid, name, notes, status, created_at, updated_at`),
		tid, phone, jid, name, time.Now()).Scan(&c.ID, &c.Phone, &c.JID, &c.Name, &c.Notes, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) GetCustomerByPhone(ctx context.Context, phone string) (*Customer, error) {
	var c Customer
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, phone, jid, name, notes, status, created_at, updated_at FROM customers WHERE phone = $1 AND tenant_id = $2`), phone, s.tid(ctx)).
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
		SELECT id, phone, jid, name, notes, status, created_at, updated_at FROM customers WHERE id = $1 AND tenant_id = $2`), id, s.tid(ctx)).
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
	args := []any{s.tid(ctx)}
	if search != "" {
		// LIKE — kecocokan parsial di Postgres.
		q += ` WHERE tenant_id = $1 AND (LOWER(phone) LIKE LOWER($2) OR LOWER(name) LIKE LOWER($3) OR LOWER(notes) LIKE LOWER($4))`
		args = append(args, "%"+search+"%", "%"+search+"%", "%"+search+"%")
	} else {
		q += ` WHERE tenant_id = $1`
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
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE customers SET notes = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`), notes, time.Now(), id, s.tid(ctx))
	return err
}

func (s *Store) SetCustomerStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE customers SET status = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`), status, time.Now(), id, s.tid(ctx))
	return err
}

// ---------- Products ----------

func (s *Store) ListProducts(ctx context.Context, activeOnly bool) ([]Product, error) {
	q := `SELECT id, name, description, price, image_path, stock, is_active, purchase_link, created_at, updated_at FROM products`
	args := []any{s.tid(ctx)}
	if activeOnly {
		q += ` WHERE tenant_id = $1 AND is_active = true`
	} else {
		q += ` WHERE tenant_id = $1`
	}
	q += ` ORDER BY id ASC`
	rows, err := s.db.QueryContext(ctx, s.q(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.ImagePath, &p.Stock, &p.IsActive,
			&p.PurchaseLink, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProduct(ctx context.Context, id int64) (*Product, error) {
	var p Product
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT id, name, description, price, image_path, stock, is_active, purchase_link, created_at, updated_at FROM products WHERE id = $1 AND tenant_id = $2`), id, s.tid(ctx)).
		Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.ImagePath, &p.Stock, &p.IsActive,
			&p.PurchaseLink, &p.CreatedAt, &p.UpdatedAt)
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
		INSERT INTO products (tenant_id, name, description, price, image_path, stock, is_active, purchase_link)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`),
		s.tid(ctx), p.Name, p.Description, p.Price, p.ImagePath, p.Stock, p.IsActive, p.PurchaseLink).Scan(&id)
	return id, err
}

func (s *Store) UpdateProduct(ctx context.Context, p *Product) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		UPDATE products SET name=$1, description=$2, price=$3, image_path=$4, stock=$5, is_active=$6,
			purchase_link=$7, updated_at=$8
		WHERE id=$9 AND tenant_id=$10`),
		p.Name, p.Description, p.Price, p.ImagePath, p.Stock, p.IsActive,
		p.PurchaseLink, time.Now(), p.ID, s.tid(ctx))
	return err
}

func (s *Store) DeleteProduct(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM products WHERE id = $1 AND tenant_id = $2`), id, s.tid(ctx))
	return err
}

// ---------- Orders ----------

func (s *Store) CreateOrder(ctx context.Context, customerID int64, address, deliveryType string, deliveryFee int64, items []OrderItem) (*Order, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Nomor urut order: counter tabel order_seq per tenant
	// (menggantikan sequence Postgres; unik per tenant).
	var seq int64
	if err := tx.QueryRowContext(ctx, s.q(`
		INSERT INTO order_seq (id, tenant_id, val) VALUES (1, $1, 1)
		ON CONFLICT (id, tenant_id) DO UPDATE SET val = order_seq.val + 1
		RETURNING val`), s.tid(ctx)).Scan(&seq); err != nil {
		return nil, err
	}
	orderNumber := fmt.Sprintf("INV-%s-%04d", time.Now().Format("20060102"), seq)

	var total int64
	for _, it := range items {
		total += it.Price * int64(it.Qty)
	}
	if deliveryType != "ambil" && deliveryFee > 0 {
		total += deliveryFee
	}

	var o Order
	err = tx.QueryRowContext(ctx, s.q(`
		INSERT INTO orders (tenant_id, order_number, customer_id, address, total, delivery_type, delivery_fee)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, order_number, customer_id, status, total, address, delivery_type, delivery_fee, note, created_at, updated_at`),
		s.tid(ctx), orderNumber, customerID, address, total, deliveryType, deliveryFee).
		Scan(&o.ID, &o.OrderNumber, &o.CustomerID, &o.Status, &o.Total, &o.Address, &o.DeliveryType, &o.DeliveryFee, &o.Note, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		// Kurangi stok (transaksional). Stok < 0 berarti tak terbatas: tidak
		// dikurangi dan tidak pernah gagal. Bila stok terbatas dan tidak
		// mencukupi, seluruh order dibatalkan (rollback).
		if it.ProductID > 0 {
			res, err := tx.ExecContext(ctx, s.q(`
				UPDATE products
				SET stock = CASE WHEN stock < 0 THEN stock ELSE stock - $1 END,
				    updated_at = $2
				WHERE id = $3 AND tenant_id = $4 AND (stock < 0 OR stock >= $1)`),
				it.Qty, time.Now(), it.ProductID, s.tid(ctx))
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, fmt.Errorf("stok produk %d tidak mencukupi (%s)", it.ProductID, it.ProductName)
			}
		}
		if _, err := tx.ExecContext(ctx, s.q(`
			INSERT INTO order_items (tenant_id, order_id, product_id, product_name, price, qty)
			VALUES ($1, $2, $3, $4, $5, $6)`),
			s.tid(ctx), o.ID, it.ProductID, it.ProductName, it.Price, it.Qty); err != nil {
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
		SELECT o.id, o.order_number, o.customer_id, o.status, o.total, o.address, o.delivery_type, o.delivery_fee, o.note, o.created_at, o.updated_at,
		       c.phone, c.name, c.status AS customer_status
		FROM orders o JOIN customers c ON c.id = o.customer_id`
	args := []any{s.tid(ctx), s.tid(ctx)}
	q += ` WHERE o.tenant_id = $1 AND c.tenant_id = $2`
	if status != "" {
		q += ` AND o.status = $3`
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
		if err := rows.Scan(&o.ID, &o.OrderNumber, &o.CustomerID, &o.Status, &o.Total, &o.Address,
			&o.DeliveryType, &o.DeliveryFee, &o.Note, &o.CreatedAt, &o.UpdatedAt, &c.Phone, &c.Name, &c.Status); err != nil {
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
		SELECT o.id, o.order_number, o.customer_id, o.status, o.total, o.address, o.delivery_type, o.delivery_fee, o.note, o.created_at, o.updated_at,
		       c.phone, c.name, c.status
		FROM orders o JOIN customers c ON c.id = o.customer_id WHERE o.id = $1 AND o.tenant_id = $2 AND c.tenant_id = $3`), id, s.tid(ctx), s.tid(ctx)).
		Scan(&o.ID, &o.OrderNumber, &o.CustomerID, &o.Status, &o.Total, &o.Address,
			&o.DeliveryType, &o.DeliveryFee, &o.Note, &o.CreatedAt, &o.UpdatedAt, &c.Phone, &c.Name, &c.Status)
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
		SELECT id, order_id, product_id, product_name, price, qty FROM order_items WHERE order_id = $1 AND tenant_id = $2`), orderID, s.tid(ctx))
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
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE orders SET status = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`), status, time.Now(), id, s.tid(ctx))
	return err
}

func (s *Store) DashboardStats(ctx context.Context) (*DashboardStats, error) {
	var st DashboardStats
	startOfDay := time.Now().Truncate(24 * time.Hour)
	tid := s.tid(ctx)
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT
			(SELECT COUNT(*) FROM orders WHERE tenant_id = $1 AND created_at >= $2),
			(SELECT COALESCE(SUM(total),0) FROM orders WHERE tenant_id = $3 AND created_at >= $4 AND status != 'batal'),
			(SELECT COUNT(*) FROM customers WHERE tenant_id = $5),
			(SELECT COUNT(*) FROM orders WHERE tenant_id = $6 AND status = 'baru')`),
		tid, startOfDay, tid, startOfDay, tid, tid).Scan(&st.OrdersToday, &st.RevenueToday, &st.TotalCustomers, &st.PendingOrders)
	return &st, err
}

// ---------- Chat messages ----------

func (s *Store) SaveChatMessage(ctx context.Context, m *ChatMessage) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO chat_messages (tenant_id, customer_id, direction, message_type, body, media_url, wa_message_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`),
		s.tid(ctx), m.CustomerID, m.Direction, m.MessageType, m.Body, m.MediaURL, m.WAMessageID)
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
		WHERE cm.customer_id = $1 AND cm.tenant_id = $2 AND c.tenant_id = $3 ORDER BY cm.created_at ASC LIMIT $4`), customerID, s.tid(ctx), s.tid(ctx), limit)
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
	// Subquery MAX(id) untuk mengambil pesan terakhir tiap customer.
	rows, err := s.db.QueryContext(ctx, s.q(`
		SELECT cm.customer_id, c.phone, c.name, cm.body, cm.direction, cm.created_at
		FROM chat_messages cm JOIN customers c ON c.id = cm.customer_id
		WHERE cm.tenant_id = $1 AND c.tenant_id = $2
		  AND cm.id IN (SELECT MAX(id) FROM chat_messages WHERE tenant_id = $3 GROUP BY customer_id)
		ORDER BY cm.created_at DESC LIMIT 200`), s.tid(ctx), s.tid(ctx), s.tid(ctx))
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
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, keyword, reply, is_active, created_at FROM quick_replies WHERE tenant_id = $1 ORDER BY keyword ASC`), s.tid(ctx))
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
		WHERE keyword = $1 AND is_active = true AND tenant_id = $2`), strings.ToLower(strings.TrimSpace(keyword)), s.tid(ctx)).
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
		INSERT INTO quick_replies (tenant_id, keyword, reply, is_active) VALUES ($1, $2, $3, $4) RETURNING id`),
		s.tid(ctx), strings.ToLower(strings.TrimSpace(q.Keyword)), q.Reply, q.IsActive).Scan(&id)
	return id, err
}

func (s *Store) UpdateQuickReply(ctx context.Context, q *QuickReply) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		UPDATE quick_replies SET keyword = $1, reply = $2, is_active = $3 WHERE id = $4 AND tenant_id = $5`),
		strings.ToLower(strings.TrimSpace(q.Keyword)), q.Reply, q.IsActive, q.ID, s.tid(ctx))
	return err
}

func (s *Store) DeleteQuickReply(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM quick_replies WHERE id = $1 AND tenant_id = $2`), id, s.tid(ctx))
	return err
}

// ---------- Broadcasts ----------

func (s *Store) CreateBroadcast(ctx context.Context, b *Broadcast) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO broadcasts (tenant_id, message, segment, total_targets) VALUES ($1, $2, $3, $4) RETURNING id`),
		s.tid(ctx), b.Message, b.Segment, b.TotalTargets).Scan(&id)
	return id, err
}

func (s *Store) ListBroadcasts(ctx context.Context) ([]Broadcast, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, message, segment, total_targets, sent, failed, status, created_at, finished_at
		FROM broadcasts WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 100`), s.tid(ctx))
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
		FROM broadcasts WHERE id = $1 AND tenant_id = $2`), id, s.tid(ctx)).
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
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE broadcasts SET status = 'running' WHERE id = $1 AND status = 'pending' AND tenant_id = $2`), id, s.tid(ctx))
	return err
}

func (s *Store) BroadcastProgress(ctx context.Context, id int64, sent, failed int) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE broadcasts SET sent = $2, failed = $3 WHERE id = $1 AND tenant_id = $4`), id, sent, failed, s.tid(ctx))
	return err
}

func (s *Store) FinishBroadcast(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE broadcasts SET status = $2, finished_at = $3 WHERE id = $1 AND tenant_id = $4`), id, status, time.Now(), s.tid(ctx))
	return err
}

// ---------- Order sessions ----------

func (s *Store) GetOrderSession(ctx context.Context, customerID int64) (*OrderSession, error) {
	var os OrderSession
	var itemsJSON string
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT customer_id, state, product_id, qty, address, delivery_type, items, updated_at FROM order_sessions WHERE customer_id = $1 AND tenant_id = $2`), customerID, s.tid(ctx)).
		Scan(&os.CustomerID, &os.State, &os.ProductID, &os.Qty, &os.Address, &os.DeliveryType, &itemsJSON, &os.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	os.Items = []OrderItem{}
	if itemsJSON != "" {
		_ = json.Unmarshal([]byte(itemsJSON), &os.Items)
	}
	return &os, nil
}

func (s *Store) UpsertOrderSession(ctx context.Context, os *OrderSession) error {
	itemsJSON, err := json.Marshal(os.Items)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.q(`
		INSERT INTO order_sessions (tenant_id, customer_id, state, product_id, qty, address, delivery_type, items, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, customer_id) DO UPDATE SET
			state = EXCLUDED.state, product_id = EXCLUDED.product_id, qty = EXCLUDED.qty,
			address = EXCLUDED.address, delivery_type = EXCLUDED.delivery_type,
			items = EXCLUDED.items, updated_at = EXCLUDED.updated_at`),
		s.tid(ctx), os.CustomerID, os.State, os.ProductID, os.Qty, os.Address, os.DeliveryType, string(itemsJSON), time.Now())
	return err
}

func (s *Store) DeleteOrderSession(ctx context.Context, customerID int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM order_sessions WHERE customer_id = $1 AND tenant_id = $2`), customerID, s.tid(ctx))
	return err
}

// ---------- AI usage ----------

// GetAIUsageCount mengembalikan jumlah jawaban AI hari ini untuk pelanggan.
func (s *Store) GetAIUsageCount(ctx context.Context, customerID int64, day string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.q(`
		SELECT count FROM ai_usage WHERE customer_id = $1 AND day = $2 AND tenant_id = $3`), customerID, day, s.tid(ctx)).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return n, nil
}

// IncrementAIUsage menaikkan penghitung penggunaan AI untuk pelanggan hari ini.
func (s *Store) IncrementAIUsage(ctx context.Context, customerID int64, day string) error {
	_, err := s.db.ExecContext(ctx, s.q(`
		INSERT INTO ai_usage (tenant_id, customer_id, day, count) VALUES ($1, $2, $3, 1)
		ON CONFLICT (tenant_id, customer_id, day) DO UPDATE SET count = ai_usage.count + 1`), s.tid(ctx), customerID, day)
	return err
}

// ---------- WA accounts ----------

func (s *Store) ListWAAccounts(ctx context.Context) ([]WAAccount, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, username, token, token_expires_at, device_id, is_active, created_at, updated_at
		FROM wa_accounts WHERE tenant_id = $1 ORDER BY id ASC`), s.tid(ctx))
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
		a.Username = s.dec(a.Username)
		a.Token = s.dec(a.Token)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetWAAccountByUsername(ctx context.Context, username string) (*WAAccount, error) {
	var a WAAccount
	var exp sql.NullTime
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, username, password, token, token_expires_at, device_id, is_active, created_at, updated_at
		FROM wa_accounts WHERE username = $1 AND tenant_id = $2`), username, s.tid(ctx)).
		Scan(&a.ID, &a.Username, &a.Password, &a.Token, &exp, &a.DeviceID, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.TokenExpiresAt = nullableTimePtr(exp)
	a.Username = s.dec(a.Username)
	a.Password = s.dec(a.Password)
	a.Token = s.dec(a.Token)
	return &a, nil
}

func (s *Store) GetActiveWAAccount(ctx context.Context) (*WAAccount, error) {
	var a WAAccount
	var exp sql.NullTime
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, username, password, token, token_expires_at, device_id, is_active, created_at, updated_at
		FROM wa_accounts WHERE is_active = true AND tenant_id = $1 ORDER BY id ASC LIMIT 1`), s.tid(ctx)).
		Scan(&a.ID, &a.Username, &a.Password, &a.Token, &exp, &a.DeviceID, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.TokenExpiresAt = nullableTimePtr(exp)
	a.Username = s.dec(a.Username)
	a.Password = s.dec(a.Password)
	a.Token = s.dec(a.Token)
	return &a, nil
}

func (s *Store) UpsertWAAccount(ctx context.Context, username, password, deviceID string, isActive bool) (int64, error) {
	tid := s.tid(ctx)

	// Satu device gowa hanya boleh milik satu tenant; kalau tidak, webhook
	// tidak bisa tahu pesan milik siapa (risiko data bocor antar toko).
	if deviceID != "" {
		var owner int64
		err := s.db.QueryRowContext(ctx, s.q(`
			SELECT tenant_id FROM wa_accounts WHERE device_id = $1 AND tenant_id <> $2 ORDER BY id LIMIT 1`), deviceID, tid).Scan(&owner)
		if err == nil {
			return 0, fmt.Errorf("device WA %q sudah dipakai tenant lain (tenant %d). Setiap toko butuh nomor/device WhatsApp sendiri.", deviceID, owner)
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	var id int64
	err := s.db.QueryRowContext(ctx, s.q(`
		INSERT INTO wa_accounts (tenant_id, username, password, device_id, is_active)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, username) DO UPDATE SET password = EXCLUDED.password,
			device_id = EXCLUDED.device_id, is_active = EXCLUDED.is_active, updated_at = $6
		RETURNING id`),
		tid, username, s.enc(password), deviceID, isActive, time.Now()).Scan(&id)
	return id, err
}

func (s *Store) SetWAAccountActive(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	tid := s.tid(ctx)
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE wa_accounts SET is_active = false WHERE tenant_id = $1`), tid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE wa_accounts SET is_active = true WHERE id = $1 AND tenant_id = $2`), id, tid); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateWAToken(ctx context.Context, id int64, token string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE wa_accounts SET token = $1, token_expires_at = $2, updated_at = $3 WHERE id = $4 AND tenant_id = $5`),
		s.enc(token), expiresAt, time.Now(), id, s.tid(ctx))
	return err
}

// ---------- Settings ----------

func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT key, value FROM settings WHERE tenant_id = $1`), s.tid(ctx))
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
		INSERT INTO settings (tenant_id, key, value) VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id, key) DO UPDATE SET value = EXCLUDED.value`), s.tid(ctx), key, value)
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
		FROM knowledge_docs WHERE tenant_id = $1 ORDER BY created_at DESC`), s.tid(ctx))
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
		INSERT INTO knowledge_docs (tenant_id, filename, file_type, size_bytes, chunk_count, data)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`),
		s.tid(ctx), doc.Filename, doc.FileType, doc.SizeBytes, doc.ChunkCount, data).Scan(&id)
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
		if _, err := tx.ExecContext(ctx, s.q(`INSERT INTO knowledge_chunks (tenant_id, doc_id, content) VALUES ($1, $2, $3)`), s.tid(ctx), docID, ch); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.q(`UPDATE knowledge_docs SET chunk_count = $2 WHERE id = $1 AND tenant_id = $3`), docID, len(chunks), s.tid(ctx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteKnowledgeDoc(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM knowledge_docs WHERE id = $1 AND tenant_id = $2`), id, s.tid(ctx))
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

	// 1) Full-text search Postgres (to_tsvector).
	rows, err := s.db.QueryContext(ctx, s.q(`
			SELECT id, content FROM knowledge_chunks
			WHERE tenant_id = $1 AND to_tsvector('simple', content) @@ plainto_tsquery('simple', $2)
			ORDER BY ts_rank(to_tsvector('simple', content), plainto_tsquery('simple', $2)) DESC
			LIMIT 20`), s.tid(ctx), q)
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

	// 2) Cadangan: LIKE bila full-text tidak menemukan cukup.
	if len(candidates) < limit {
		var where strings.Builder
		args := []any{s.tid(ctx)}
		where.WriteString("tenant_id = $1 AND (")
		for i, w := range words {
			if i > 0 {
				where.WriteString(" OR ")
			}
			fmt.Fprintf(&where, "LOWER(content) LIKE LOWER($%d)", i+2)
			args = append(args, "%"+w+"%")
		}
		where.WriteString(")")
		rows2, err := s.db.QueryContext(ctx, s.q(fmt.Sprintf(`
			SELECT id, content FROM knowledge_chunks WHERE %s ORDER BY id DESC LIMIT 20`,
			where.String())), args...)
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
