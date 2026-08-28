// Package db 提供数据库连接、迁移与 sqlc 生成的类型化查询。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // goose 迁移走 database/sql 驱动
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Connect 建立连接池并跑完全部迁移。
// Fail-Fast：连不上或迁移失败直接返回错误，阻止服务启动。
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("解析数据库配置: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	if err := migrate(url); err != nil {
		pool.Close()
		return nil, fmt.Errorf("执行迁移: %w", err)
	}
	return pool, nil
}

func migrate(url string) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return goose.Up(sqlDB, "migrations")
}
