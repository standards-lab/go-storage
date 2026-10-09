#!/usr/bin/env bash
# seaweedfs starts or stops the s3 module's acceptance harness: one SeaweedFS
# container running `weed mini`, with only its S3 gateway published, on
# 127.0.0.1 at SEAWEEDFS_PORT (default 8333). Run it through
# `mise run seaweedfs:start` and `mise run seaweedfs:stop`; CI's acceptance
# job runs it too.
#
# The image is pinned once, on the image: line of CI's "start SeaweedFS"
# step, where `mise run currency` finds it. CI passes that line to this
# script as $image; run locally, the script reads the same line from the
# workflow, so the harness and CI never drift apart.
#
# The gateway's admin identity is the access key admin with the secret
# secret, which SeaweedFS reads from AWS_ACCESS_KEY_ID and
# AWS_SECRET_ACCESS_KEY at startup. -s3.autoCreateBucket=false turns off
# SeaweedFS's default of creating a missing bucket on an admin's upload, so a
# write to a missing bucket fails as it does on AWS.
#
# start waits until a request signed with that identity lists the buckets,
# which proves the gateway, its filer, and the credential together; the
# gateway answers unsigned requests before the credential is loaded. The
# container keeps its data inside itself and is removed when it stops, so
# every start begins empty.
set -euo pipefail

workflow=${MISE_PROJECT_ROOT:-$(dirname "$0")/..}/.github/workflows/ci.yml
name=go-storage-seaweedfs
port=${SEAWEEDFS_PORT:-8333}
endpoint=http://127.0.0.1:$port
wait_seconds=60

# pinned_image prints the SeaweedFS image named on the workflow's image: line.
pinned_image() {
	local ref
	ref=$(grep -o '^ *image: *chrislusf/seaweedfs:[^ ]*' "$workflow" | sed 's/^ *image: *//' || true)
	if [ -z "$ref" ]; then
		echo "seaweedfs: no chrislusf/seaweedfs image: line in $workflow" >&2
		return 1
	fi
	echo "$ref"
}

start() {
	if [ -n "$(docker ps -q --filter "name=^${name}$")" ]; then
		echo "seaweedfs: $name is already running"
	else
		local ref=${image:-}
		[ -n "$ref" ] || ref=$(pinned_image)
		docker run --detach --rm --name "$name" \
			--publish "127.0.0.1:$port:8333" \
			--env AWS_ACCESS_KEY_ID=admin \
			--env AWS_SECRET_ACCESS_KEY=secret \
			"$ref" mini -dir=/data -s3.autoCreateBucket=false >/dev/null
		echo "seaweedfs: started $name from $ref"
	fi

	for _ in $(seq "$wait_seconds"); do
		code=$(curl --silent --output /dev/null --write-out '%{http_code}' \
			--aws-sigv4 "aws:amz:us-east-1:s3" --user admin:secret \
			"$endpoint/" || true)
		if [ "$code" = 200 ]; then
			echo "seaweedfs: S3 gateway ready at $endpoint"
			return 0
		fi
		sleep 1
	done
	echo "seaweedfs: S3 gateway not ready at $endpoint after ${wait_seconds}s; container log:" >&2
	docker logs --tail 50 "$name" >&2 || true
	return 1
}

stop() {
	if [ -n "$(docker ps -aq --filter "name=^${name}$")" ]; then
		docker stop "$name" >/dev/null
		echo "seaweedfs: stopped $name"
	else
		echo "seaweedfs: $name is not running"
	fi
}

case "${1:-}" in
start) start ;;
stop) stop ;;
*)
	echo "usage: $0 start|stop" >&2
	exit 2
	;;
esac
