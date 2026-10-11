#!/bin/sh
set -eu

config_file="${CONFIG_FILE:-/config/config.yaml}"
config_dir="$(dirname "$config_file")"
data_dir="${DATA_DIR:-/config/cache}"

if [ ! -f "$config_file" ]; then
  echo "请先创建 $config_file 并填写管理员账号密码，再启动容器。" >&2
  exit 1
fi

if [ "$(id -u)" = 0 ]; then
  # Follow the mounted directory's owner so host-side editing still works.
  # Root-owned deployments use the image's unprivileged account instead.
  run_uid="$(stat -c %u "$config_dir")"
  run_gid="$(stat -c %g "$config_dir")"
  if [ "$run_uid" = 0 ]; then
    run_uid=10001
    run_gid=10001
  fi
  mkdir -p "$data_dir"
  # The mount can also contain compose.yaml and other deployment files.
  # Only the parent directory itself, configuration and cache need ownership.
  chown -h "$run_uid:$run_gid" "$config_dir" "$config_file"
  chown -Rh "$run_uid:$run_gid" "$data_dir"
  exec su-exec "$run_uid:$run_gid" "$0" "$@"
fi

umask 077
chmod 600 "$config_file"
exec /usr/local/bin/rumiho "$@"
