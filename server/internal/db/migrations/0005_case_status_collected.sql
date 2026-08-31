-- +goose Up
-- 用例状态新增 collected（已采集·待评估）：token 计量检定这类跨行断言的检测项，
-- 采集成功的行在阶段二评估前以中间态实时落库（只展示原始计数、不给结论），
-- 评估后被终态幂等覆盖；任务终态后不应残留 collected 行
alter table task_case_results drop constraint task_case_results_status_check;
alter table task_case_results add constraint task_case_results_status_check
    check (status in ('passed', 'rejected', 'violated', 'collected'));
