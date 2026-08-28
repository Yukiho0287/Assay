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

-- name: CreateTask :one
insert into tasks (kind, channel_id, target, probes, params, progress_total, created_by)
values ($1, $2, $3, $4, $5, $6, $7)
returning id, created_at;

-- name: SetTaskRiverJobID :exec
update tasks set river_job_id = $2 where id = $1;

-- name: GetTask :one
select t.id, t.kind, t.status, t.channel_id, t.target, t.probes, t.params,
       t.progress_total, t.progress_done, t.error, t.created_at, t.started_at, t.finished_at,
       u.username as created_by_name
from tasks t
left join users u on u.id = t.created_by
where t.id = $1;

-- name: ListTasks :many
select t.id, t.kind, t.status, t.channel_id, t.target, t.probes, t.params,
       t.progress_total, t.progress_done, t.error, t.created_at, t.started_at, t.finished_at,
       u.username as created_by_name
from tasks t
left join users u on u.id = t.created_by
where t.kind = $1
order by t.created_at desc
limit $2 offset $3;

-- name: CountTasks :one
select count(*) from tasks where kind = $1;

-- name: MarkTaskRunning :execrows
-- 状态守卫：只允许 queued→running（River 重试进来时若已是终态则不重置）
update tasks set status = 'running', started_at = coalesce(started_at, now())
where id = $1 and status in ('queued', 'running');

-- name: FinishTask :execrows
-- 终态不可逆：running 之外（已 canceled/failed）不允许再改
update tasks set status = $2, error = $3, finished_at = now()
where id = $1 and status = 'running';

-- name: UpdateTaskProgress :exec
update tasks set progress_total = $2, progress_done = $3 where id = $1;

-- name: CancelQueuedTask :execrows
-- 仅排队中可取消（running 的 MVP 不支持中断）
update tasks set status = 'canceled', finished_at = now()
where id = $1 and status = 'queued';

-- name: FailOrphanTasks :execrows
-- 启动孤儿清扫：river_job 已不存在/已终结但任务还挂在 running 的，标记失败
update tasks set status = 'failed', error = sqlc.arg('error'), finished_at = now()
where status = 'running' and id = any (sqlc.arg('ids')::uuid[]);

-- name: ListRunningTasks :many
select id, river_job_id from tasks where status = 'running';

-- name: DeleteTaskCaseResults :exec
delete from task_case_results where task_id = $1;

-- name: UpsertTaskCaseResult :exec
insert into task_case_results
    (task_id, probe, suite, line, mode, selection_reason, status, message,
     http_status, latency_ms, arguments, attempts)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
on conflict (task_id, probe, suite, line, mode) do update set
    selection_reason = excluded.selection_reason,
    status           = excluded.status,
    message          = excluded.message,
    http_status      = excluded.http_status,
    latency_ms       = excluded.latency_ms,
    arguments        = excluded.arguments,
    attempts         = excluded.attempts;

-- name: ListTaskCaseResults :many
select probe, suite, line, mode, selection_reason, status, message,
       http_status, latency_ms, arguments, attempts
from task_case_results
where task_id = $1
  and (sqlc.narg('status')::text is null or status = sqlc.narg('status')::text)
order by probe, suite, line, mode;

-- name: AggregateTaskCaseResults :many
-- 三个维度一次取回：dimension=mode/reason 各一组分桶，前端聚合总数
select mode as bucket_mode, selection_reason as bucket_reason, status, count(*) as n
from task_case_results
where task_id = $1
group by mode, selection_reason, status;
