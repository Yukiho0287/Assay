-- +goose Up
-- 渠道表：一个 LLM 供应商接入点（base_url + key + 协议集合），下挂模型条目
-- api_key 写后不可回读（接口只返回 key_prefix 脱敏前缀）；last_test 存最近一次连通测试结果快照
create table channels (
    id         uuid primary key default gen_random_uuid(),
    name       text not null unique,
    base_url   text not null,
    api_key    text not null,
    key_prefix text not null,
    protocols  text[] not null,
    currency   text not null default 'USD',
    note       text not null default '',
    disabled   boolean not null default false,
    last_test  jsonb,
    created_at timestamptz not null default now()
);

-- 模型条目：渠道下的可测模型（模型名 + 三个可空单价，单位：每 1M token）
-- 价格约束与渠道删除级联都在库层兜底：输入/输出价成对出现，缓存读价依赖前两者
create table channel_models (
    id                 uuid primary key default gen_random_uuid(),
    channel_id         uuid not null references channels (id) on delete cascade,
    name               text not null,
    input_price        double precision,
    output_price       double precision,
    cached_input_price double precision,
    created_at         timestamptz not null default now(),
    unique (channel_id, name),
    check ((input_price is null) = (output_price is null)),
    check (cached_input_price is null or input_price is not null)
);

-- +goose Down
drop table channel_models;
drop table channels;
