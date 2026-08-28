-- name: CountUsers :one
select count(*) from users;

-- name: CreateUser :one
insert into users (username, password_hash, role)
values ($1, $2, $3)
returning id, username, role, created_at;

-- name: GetUserByUsername :one
select id, username, password_hash, role, created_at
from users
where username = $1;

-- name: CreateSession :exec
insert into sessions (token_hash, user_id, expires_at)
values ($1, $2, $3);

-- name: GetSessionUser :one
select u.id, u.username, u.role
from sessions s
join users u on u.id = s.user_id
where s.token_hash = $1 and s.expires_at > now();

-- name: DeleteSession :exec
delete from sessions where token_hash = $1;

-- name: DeleteExpiredSessions :execrows
delete from sessions where expires_at <= now();
