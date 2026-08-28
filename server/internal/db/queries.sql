-- name: CountUsers :one
select count(*) from users;

-- name: CreateUser :one
insert into users (username, password_hash, role_id)
values ($1, $2, $3)
returning id;

-- name: GetUser :one
select u.id, u.username, u.role_id, r.name as role_name, u.created_at
from users u
join roles r on r.id = u.role_id
where u.id = $1;

-- name: GetUserByUsername :one
select u.id, u.username, u.password_hash
from users u
where u.username = $1;

-- name: GetUserPasswordHash :one
select password_hash from users where id = $1;

-- name: ListUsers :many
select u.id, u.username, u.role_id, r.name as role_name, u.created_at
from users u
join roles r on r.id = u.role_id
order by u.created_at;

-- name: UpdateUserRole :execrows
update users set role_id = $2 where id = $1;

-- name: UpdateUserPassword :execrows
update users set password_hash = $2 where id = $1;

-- name: DeleteUser :execrows
delete from users where id = $1;

-- name: GetRole :one
select id, name, built_in, permissions from roles where id = $1;

-- name: GetRoleByName :one
select id, name, built_in, permissions from roles where name = $1;

-- name: ListRoles :many
select id, name, built_in, permissions from roles order by created_at;

-- name: CreateRole :one
insert into roles (name, permissions)
values ($1, $2)
returning id, name, built_in, permissions;

-- name: UpdateRole :one
update roles set name = $2, permissions = $3
where id = $1
returning id, name, built_in, permissions;

-- name: DeleteRole :execrows
delete from roles where id = $1;

-- name: CountUsersByRole :one
select count(*) from users where role_id = $1;

-- name: CreateSession :exec
insert into sessions (token_hash, user_id, expires_at)
values ($1, $2, $3);

-- name: GetSessionUser :one
select u.id, u.username, r.name as role_name, r.permissions
from sessions s
join users u on u.id = s.user_id
join roles r on r.id = u.role_id
where s.token_hash = $1 and s.expires_at > now();

-- name: DeleteSession :exec
delete from sessions where token_hash = $1;

-- name: DeleteUserSessions :exec
delete from sessions where user_id = $1;

-- name: DeleteOtherUserSessions :exec
delete from sessions where user_id = $1 and token_hash <> $2;

-- name: DeleteExpiredSessions :execrows
delete from sessions where expires_at <= now();

-- name: ListChannels :many
select c.id, c.name, c.base_url, c.key_prefix, c.protocols, c.currency, c.note,
       c.disabled, c.last_test, c.created_at,
       (select count(*) from channel_models m where m.channel_id = c.id) as model_count
from channels c
order by c.created_at;

-- name: GetChannel :one
select c.id, c.name, c.base_url, c.key_prefix, c.protocols, c.currency, c.note,
       c.disabled, c.last_test, c.created_at,
       (select count(*) from channel_models m where m.channel_id = c.id) as model_count
from channels c
where c.id = $1;

-- name: GetChannelSecret :one
-- 连通测试专用：整个代码库唯一读取 api_key 明文的查询，结果绝不进任何响应体
select id, base_url, api_key, protocols from channels where id = $1;

-- name: CreateChannel :one
insert into channels (name, base_url, api_key, key_prefix, protocols, currency, note)
values ($1, $2, $3, $4, $5, $6, $7)
returning id;

-- name: UpdateChannel :execrows
update channels set
    name       = coalesce(sqlc.narg('name'), name),
    base_url   = coalesce(sqlc.narg('base_url'), base_url),
    api_key    = coalesce(sqlc.narg('api_key'), api_key),
    key_prefix = coalesce(sqlc.narg('key_prefix'), key_prefix),
    protocols  = coalesce(sqlc.narg('protocols'), protocols),
    currency   = coalesce(sqlc.narg('currency'), currency),
    note       = coalesce(sqlc.narg('note'), note),
    disabled   = coalesce(sqlc.narg('disabled'), disabled)
where id = sqlc.arg('id');

-- name: UpdateChannelLastTest :execrows
update channels set last_test = $2 where id = $1;

-- name: DeleteChannel :execrows
delete from channels where id = $1;

-- name: ListChannelModels :many
select id, name, input_price, output_price, cached_input_price, created_at
from channel_models
where channel_id = $1
order by created_at;

-- name: GetChannelModel :one
select id, name, input_price, output_price, cached_input_price, created_at
from channel_models
where id = $1 and channel_id = $2;

-- name: CreateChannelModel :one
insert into channel_models (channel_id, name, input_price, output_price, cached_input_price)
values ($1, $2, $3, $4, $5)
returning id, name, input_price, output_price, cached_input_price, created_at;

-- name: UpdateChannelModel :one
update channel_models
set name = $3, input_price = $4, output_price = $5, cached_input_price = $6
where id = $1 and channel_id = $2
returning id, name, input_price, output_price, cached_input_price, created_at;

-- name: DeleteChannelModel :execrows
delete from channel_models where id = $1 and channel_id = $2;
