-- +goose Up
-- 连通测试历史：一行 =（一次测试 × 一协议）；tested_at 显式传入，
-- 同一次测试的各协议行时间戳严格一致，前端按 tested_at 分组即可还原一次测试
create table connectivity_results (
    id          bigint generated always as identity primary key,
    channel_id  uuid not null references channels (id) on delete cascade,
    model       text not null,
    source      text not null check (source in ('manual', 'scheduled')),
    protocol    text not null,
    ok          boolean not null,
    http_status int,
    ttft_ms     int,
    error       text,
    tested_at   timestamptz not null
);

create index connectivity_results_channel_time on connectivity_results (channel_id, tested_at desc);

-- 渠道定时探活配置：interval 为空 = 关闭；探活模型被删时 set null 令调度自动停摆
alter table channels
    add column probe_interval_minutes int check (probe_interval_minutes between 1 and 1440),
    add column probe_model_id uuid references channel_models (id) on delete set null;

-- +goose Down
alter table channels drop column probe_model_id, drop column probe_interval_minutes;
drop table connectivity_results;
