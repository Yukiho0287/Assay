-- +goose Up
-- 用户表：不开放注册，账号由 admin 创建
create table users (
    id            uuid primary key default gen_random_uuid(),
    username      text not null unique,
    password_hash text not null,
    role          text not null check (role in ('admin', 'member')),
    created_at    timestamptz not null default now()
);

-- 会话表：cookie 里存随机 token，库里只存其 SHA-256 哈希（泄库不泄会话）
create table sessions (
    token_hash bytea primary key,
    user_id    uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

create index sessions_expires_at_idx on sessions (expires_at);

-- +goose Down
drop table sessions;
drop table users;
