package live

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

// PGListener listens on the sink trigger's channel over a dedicated
// PostgreSQL connection per subscription.
type PGListener struct {
	databaseURL string
}

// NewPGListener returns a listener connecting to databaseURL.
func NewPGListener(databaseURL string) *PGListener {
	return &PGListener{databaseURL: databaseURL}
}

// Listen connects and runs LISTEN on migrations.NotifyChannel.
func (l *PGListener) Listen(ctx context.Context) (Subscription, error) {
	conn, err := pgx.Connect(ctx, l.databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if _, err := conn.Exec(ctx, "LISTEN "+migrations.NotifyChannel); err != nil {
		closeConn(ctx, conn)
		return nil, fmt.Errorf("listen: %w", err)
	}
	return &pgSubscription{conn: conn}, nil
}

type pgSubscription struct {
	conn *pgx.Conn
}

func (s *pgSubscription) Wait(ctx context.Context) (int64, error) {
	n, err := s.conn.WaitForNotification(ctx)
	if err != nil {
		return 0, err
	}
	h, err := strconv.ParseInt(n.Payload, 10, 64)
	if err != nil {
		log.WithField("payload", n.Payload).Warn("live indexer: notification without a height")
		return 0, nil
	}
	return h, nil
}

func (s *pgSubscription) Close() {
	closeConn(context.Background(), s.conn)
}

func closeConn(ctx context.Context, conn *pgx.Conn) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_ = conn.Close(closeCtx)
}
