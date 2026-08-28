#!/usr/bin/env bash
# Assay 部署接收端：由 GitHub Actions 经 SSH 调用。
# 该脚本通过 authorized_keys 的 command= 强制命令绑定到部署专用 key——
# 那把 key 连上来只能执行本脚本，别的什么都干不了。
#
# 协议：SSH_ORIGINAL_COMMAND = "deploy <tag> <sha256>"，stdin = assay-<tag>-linux-amd64.tar.gz
# 路径：无待应用迁移 → 直接重启（socket activation 下连接排队，接近零停机）；
#       有迁移 → pg_dump 备份 → 停服 → 起新版（启动时自动迁移）→ 健康检查。
# 失败：健康检查不过 → 符号链接切回旧版重启（二进制回滚；数据库只 roll-forward）。
set -euo pipefail

ROOT=/opt/assay
ENV_FILE=$ROOT/env
VERSION_URL=http://127.0.0.1:8321/api/version

read -r action tag sum _rest <<< "${SSH_ORIGINAL_COMMAND:-}" || true
if [[ "${action:-}" != "deploy" || ! "${tag:-}" =~ ^v[0-9][A-Za-z0-9.+~-]*$ || ! "${sum:-}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "拒绝：非法部署命令" >&2
  exit 1
fi

echo "[deploy] 接收 $tag"
tmp=$(mktemp -d "$ROOT/tmp.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
cat > "$tmp/assay.tar.gz"

echo "$sum  $tmp/assay.tar.gz" | sha256sum -c - >/dev/null \
  || { echo "拒绝：sha256 校验失败" >&2; exit 1; }

rel=$ROOT/releases/$tag
mkdir -p "$rel"
tar -xzf "$tmp/assay.tar.gz" -C "$rel"
[[ -x "$rel/assay" ]] || { echo "拒绝：包内缺少 assay 可执行文件" >&2; exit 1; }
chown -R assay:assay "$rel"
# SELinux enforcing 时 systemd 需要 bin_t 才能执行 /opt 下的二进制
if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
  chcon -t bin_t "$rel/assay"
fi

prev=$(readlink -f "$ROOT/current" 2>/dev/null || true)

set -a; source "$ENV_FILE"; set +a
pending=$("$rel/assay" migrate pending)
echo "[deploy] 待应用迁移: $pending"

if [[ "$pending" != "0" ]]; then
  echo "[deploy] 迁移路径：先备份数据库再短停机"
  backup=$ROOT/backups/assay-$(date +%Y%m%d-%H%M%S)-pre-$tag.sql.gz
  docker exec assay-postgres pg_dump -U assay assay | gzip > "$backup"
  echo "[deploy] 备份完成 $backup ($(du -h "$backup" | cut -f1))"
  systemctl stop assay
fi

ln -sfn "$rel" "$ROOT/current"
systemctl restart assay

# 健康检查：15 秒内 /api/version 必须返回新 tag，否则回滚
ok=""
for _ in $(seq 1 15); do
  sleep 1
  if curl -sf --max-time 2 "$VERSION_URL" | grep -q "\"$tag\""; then
    ok=1
    break
  fi
done

if [[ -z "$ok" ]]; then
  echo "[deploy] 健康检查失败，回滚到 ${prev:-<无上一版本>}" >&2
  journalctl -u assay -n 30 --no-pager >&2 || true
  if [[ -n "$prev" && -d "$prev" ]]; then
    ln -sfn "$prev" "$ROOT/current"
    systemctl restart assay
  fi
  exit 1
fi

echo "[deploy] $tag 上线成功（模式：$([[ "$pending" == "0" ]] && echo 热重启 || echo 迁移短停机)）"
# 只保留最近 5 个版本目录
ls -1dt "$ROOT"/releases/*/ | tail -n +6 | xargs -r rm -rf
