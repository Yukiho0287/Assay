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
