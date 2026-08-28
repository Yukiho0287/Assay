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
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
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

// Migrate 应用全部未执行的迁移（也供 assay migrate up 子命令使用）：
// 先跑 goose 业务迁移，再跑 River 队列自带迁移（river_job 等表）。
func Migrate(url string) error {
	sqlDB, err := openGoose(url)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		return err
	}
	return riverMigrate(url)
}

// riverMigrate 应用 River 队列的库表迁移（幂等，已最新则空跑）。
func riverMigrate(url string) error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("river 迁移连接: %w", err)
	}
	defer pool.Close()
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{})
	if err != nil {
		return fmt.Errorf("river 迁移器初始化: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{}); err != nil {
		return fmt.Errorf("river 迁移执行: %w", err)
	}
	return nil
}

// PendingMigrations 返回尚未应用的迁移数（goose 业务迁移 + River 队列迁移）。
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

	riverPending, err := pendingRiverMigrations(url)
	if err != nil {
		return 0, err
	}
	return n + riverPending, nil
}

func pendingRiverMigrations(url string) (int, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return 0, fmt.Errorf("river 迁移连接: %w", err)
	}
	defer pool.Close()
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{})
	if err != nil {
		return 0, fmt.Errorf("river 迁移器初始化: %w", err)
	}
	// river_migration 表不存在时 ExistingVersions 返回空集而非报错，全部版本都算待应用
	existing, err := migrator.ExistingVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("river 已应用版本查询: %w", err)
	}
	return len(migrator.AllVersions()) - len(existing), nil
}

func openGoose(url string) (*sql.DB, error) {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	return sql.Open("pgx", url)
}
