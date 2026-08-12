#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
remote=${REMOTE:-ec2-user@api.plugins.wago.sh}
remote_dir=${REMOTE_DIR:-/home/ec2-user/Wago/registry/backend}
remote_svc=${REMOTE_SVC:-wago-registry}
remote_store_dir=${REMOTE_STORE_DIR:-data/registry-db}
local_store_dir=${LOCAL_STORE_DIR:-"$repo_root/backend/data/local-prod-db"}
remote_api_port=${REMOTE_API_PORT:-8787}

case "$local_store_dir" in
    "$repo_root"/backend/data/*) ;;
    *)
        echo "refusing local store outside $repo_root/backend/data: $local_store_dir" >&2
        exit 1
        ;;
esac

for command in ssh scp tar mktemp go; do
    command -v "$command" >/dev/null || { echo "missing required command: $command" >&2; exit 1; }
done

if [[ ${CONFIRM_PROD_DB_COPY:-} != yes ]]; then
    if [[ ! -t 0 ]]; then
        echo "production snapshot requires confirmation; rerun in a terminal or set CONFIRM_PROD_DB_COPY=yes" >&2
        exit 1
    fi
    printf 'This briefly stops %s on %s to take a consistent DB snapshot. Continue? [y/N] ' "$remote_svc" "$remote"
    read -r answer
    [[ $answer == y || $answer == Y ]] || { echo "cancelled"; exit 1; }
fi

mkdir -p "$repo_root/backend/data"
work_dir=$(mktemp -d "$repo_root/backend/data/.prod-snapshot.XXXXXX")
archive="$work_dir/registry-db.tgz"
raw_store="$work_dir/raw-store"
local_clone="$work_dir/store"
remote_archive="/tmp/wago-registry-db-${USER:-local}-$$.tgz"

cleanup() {
    rm -rf -- "$work_dir"
    ssh "$remote" rm -f -- "$remote_archive" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "▸ creating a consistent production snapshot (brief service restart)"
ssh "$remote" bash -s -- "$remote_dir" "$remote_svc" "$remote_store_dir" "$remote_archive" "$remote_api_port" <<'REMOTE_SCRIPT'
set -euo pipefail
remote_dir=$1
service=$2
store_dir=$3
archive=$4
api_port=$5

case "$store_dir" in
    /*) store_path=$store_dir ;;
    *) store_path="${remote_dir}/${store_dir#./}" ;;
esac

[[ -d $store_path ]] || { echo "production store not found: $store_path" >&2; exit 1; }
systemctl is-active --quiet "$service" || { echo "service is not active: $service" >&2; exit 1; }

restart_needed=0
restart_service() {
    if [[ $restart_needed == 1 ]]; then
        sudo systemctl start "$service"
    fi
}
trap restart_service EXIT HUP INT TERM

sudo systemctl stop "$service"
restart_needed=1
tar -C "$store_path" -czf "$archive" .
sudo systemctl start "$service"
restart_needed=0

for _ in 1 2 3 4 5; do
    curl -fsS "http://localhost:${api_port}/api/health" >/dev/null && exit 0
    sleep 1
done
echo "service restarted but health check failed: $service" >&2
exit 1
REMOTE_SCRIPT

echo "▸ downloading production snapshot"
scp -q "$remote:$remote_archive" "$archive"
mkdir -p "$raw_store"
tar -xzf "$archive" --no-same-owner -C "$raw_store"

echo "▸ scrubbing copied OAuth credentials, verification codes, and API tokens"
(cd "$repo_root/backend" && go run ./cmd/scrub-store -source "$raw_store" -dest "$local_clone")
chmod -R u+rwX,go-rwx "$local_clone"

rm -rf -- "$local_store_dir"
mv "$local_clone" "$local_store_dir"
echo "✓ local production snapshot ready at $local_store_dir"
