-- +goose Up
-- 检测任务表：质量/稳定性两大类共用（kind 区分），target 存检测对象参数快照（证据链要求，
-- 绝不含 API key——worker 执行时按 channel_id 现读，渠道被删则任务无法重跑但历史报告自足）
create table tasks (
    id             uuid primary key default gen_random_uuid(),
    kind           text not null,
    status         text not null default 'queued',
    channel_id     uuid references channels (id) on delete set null,
    target         jsonb not null,
    probes         text[] not null,
    params         jsonb not null,
    progress_total int not null default 0,
    progress_done  int not null default 0,
    river_job_id   bigint,
    error          text,
    created_by     uuid references users (id) on delete set null,
    created_at     timestamptz not null default now(),
    started_at     timestamptz,
    finished_at    timestamptz,
    check (status in ('queued', 'running', 'succeeded', 'failed', 'canceled'))
);

create index tasks_created_at_idx on tasks (created_at desc);

-- 用例级结果：一行 = 一个（检测项 × 套件 × 行号 × 模式）槽位，重跑用 UPSERT 覆盖（末次结果为准）
-- rejected=请求被上游拒绝（HTTP/传输错误）；violated=响应到手但不合规；passed=合规
create table task_case_results (
    id               bigint generated always as identity primary key,
    task_id          uuid not null references tasks (id) on delete cascade,
    probe            text not null,
    suite            text not null,
    line             int not null,
    mode             text not null,
    selection_reason text not null,
    status           text not null,
    message          text not null default '',
    http_status      int,
    latency_ms       int,
    arguments        text,
    attempts         int not null default 1,
    created_at       timestamptz not null default now(),
    unique (task_id, probe, suite, line, mode),
    check (mode in ('non_stream', 'stream')),
    check (status in ('passed', 'rejected', 'violated'))
);

-- +goose Down
drop table task_case_results;
drop table tasks;
