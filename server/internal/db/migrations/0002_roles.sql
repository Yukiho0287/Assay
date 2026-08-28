-- +goose Up
-- 角色表：粗粒度模块权限（permissions jsonb 的键 = 模块名，值 = 是否可访问）
-- 内置角色（built_in）不可修改与删除，保证系统始终存在全权限 admin 角色
create table roles (
    id          uuid primary key default gen_random_uuid(),
    name        text not null unique,
    built_in    boolean not null default false,
    permissions jsonb not null default '{}'::jsonb,
    created_at  timestamptz not null default now()
);

insert into roles (name, built_in, permissions) values
    ('admin',  true, '{"channels": true, "quality": true, "stability": true, "users": true,  "system": true}'),
    ('member', true, '{"channels": true, "quality": true, "stability": true, "users": false, "system": false}');

-- users.role 字符串列迁移为 role_id 外键（改彻底：旧列直接删除，不留兼容层）
alter table users add column role_id uuid references roles (id);
update users set role_id = r.id from roles r where r.name = users.role;
alter table users alter column role_id set not null;
alter table users drop column role;

-- +goose Down
alter table users add column role text;
update users set role = r.name from roles r where r.id = users.role_id;
alter table users alter column role set not null;
alter table users drop column role_id;
drop table roles;
