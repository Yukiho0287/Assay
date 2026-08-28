// Package db 提供数据库连接、迁移与 sqlc 生成的类型化查询。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"math"

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
	if err := Migrate(url); err != nil {
		pool.Close()
		return nil, fmt.Errorf("执行迁移: %w", err)
	}
	return pool, nil
}

// Migrate 应用全部未执行的迁移（也供 assay migrate up 子命令使用）。
func Migrate(url string) error {
	sqlDB, err := openGoose(url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return goose.Up(sqlDB, "migrations")
}

// PendingMigrations 返回尚未应用的迁移数，不执行任何迁移。
// 部署脚本据此选择路径：0 = 热重启，>0 = 备份 + 短停机迁移。
func PendingMigrations(url string) (int, error) {
	sqlDB, err := openGoose(url)
	if err != nil {
		return 0, err
	}
	defer sqlDB.Close()

	all, err := goose.CollectMigrations("migrations", 0, math.MaxInt64)
	if err != nil {
		return 0, err
	}
	dbVersion, err := goose.EnsureDBVersion(sqlDB)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range all {
		if m.Version > dbVersion {
			n++
		}
	}
	return n, nil
}

func openGoose(url string) (*sql.DB, error) {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	return sql.Open("pgx", url)
}
