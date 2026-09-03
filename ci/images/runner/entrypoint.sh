#!/usr/bin/env bash
# Entrypoint of the qrl-tests-runner image when it runs as an in-cluster Job:
# point Kurtosis at the cluster it is running in, make sure the engine is up,
# run one lane, and leave the verdict in the report directory. Outside a
# cluster (no service-account token) it only runs the lane, for local use of
# the image against a Docker or pre-configured Kurtosis setup.
set -euo pipefail

lane="${1:-${E2E_LANE:?set E2E_LANE or pass the lane name}}"
shift $(( $# > 0 ? 1 : 0 ))

report_dir="${E2E_REPORT_DIR:-/reports}"
service_account_dir=/var/run/secrets/kubernetes.io/serviceaccount
kurtosis_config_source="${KURTOSIS_CONFIG_SOURCE:-/etc/kurtosis/kurtosis-config.yml}"
kurtosis_cluster="${KURTOSIS_CLUSTER:-}"
engine_log_retention="${KURTOSIS_ENGINE_LOG_RETENTION:-168h}"
engine_wait_seconds="${KURTOSIS_ENGINE_WAIT_SECONDS:-300}"

mkdir -p "${report_dir}"

# Job annotations are how the heartbeat workflow reports progress without a
# runner attached; every annotation failure is non-fatal.
annotate() {
	if [ -z "${JOB_NAME:-}" ] || [ -z "${JOB_NAMESPACE:-}" ]; then
		return 0
	fi
	kubectl annotate job "${JOB_NAME}" -n "${JOB_NAMESPACE}" --overwrite "$@" >/dev/null 2>&1 || true
}

finish() {
	local status=$1
	printf '{"exit_code":%d,"finished_at":"%s"}\n' "${status}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${report_dir}/job-status.json"
	if [ "${status}" -eq 0 ]; then
		annotate qrl.io/phase=finished qrl.io/result=passed
	else
		annotate qrl.io/phase=finished qrl.io/result=failed
	fi
	if [ -n "${gateway_pid:-}" ]; then
		kill "${gateway_pid}" 2>/dev/null || true
	fi
}

gateway_pid=""

configure_cluster() {
	# In-cluster credentials via a kubeconfig that references the projected
	# token file: client-go re-reads it, so the token rotating during a
	# multi-hour run does not break Kurtosis or kubectl.
	local kubeconfig="${HOME}/.kube/config"
	mkdir -p "$(dirname "${kubeconfig}")"
	cat >"${kubeconfig}" <<EOF
apiVersion: v1
kind: Config
clusters:
  - name: in-cluster
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority: ${service_account_dir}/ca.crt
users:
  - name: job
    user:
      tokenFile: ${service_account_dir}/token
contexts:
  - name: in-cluster
    context:
      cluster: in-cluster
      user: job
      namespace: $(cat "${service_account_dir}/namespace")
current-context: in-cluster
EOF
	export KUBECONFIG="${kubeconfig}"

	local config_path
	config_path=$(kurtosis config path)
	mkdir -p "$(dirname "${config_path}")"
	cp "${kurtosis_config_source}" "${config_path}"
	kurtosis analytics disable >/dev/null 2>&1 || true
	kurtosis cluster set "${kurtosis_cluster}"

	# `engine start` is idempotent: a running engine of the same version is
	# left alone, so consecutive Jobs share it.
	annotate qrl.io/phase=engine
	kurtosis engine start --version "${KURTOSIS_VERSION}" --log-retention-period "${engine_log_retention}"

	# The SDK talks to the engine through the gateway's local port-forward;
	# the network itself is reached in-cluster (DEVNET_ENDPOINT_MODE=cluster).
	kurtosis gateway >"${report_dir}/kurtosis-gateway.log" 2>&1 &
	gateway_pid=$!
	local waited=0
	until kurtosis engine status 2>/dev/null | grep -q 'Kurtosis engine is running'; do
		if [ "${waited}" -ge "${engine_wait_seconds}" ]; then
			echo "Kurtosis engine did not become reachable within ${engine_wait_seconds}s" >&2
			return 1
		fi
		sleep 5
		waited=$(( waited + 5 ))
	done
}

trap 'finish $?' EXIT
annotate qrl.io/phase=starting

if [ -f "${service_account_dir}/token" ]; then
	: "${kurtosis_cluster:?set KURTOSIS_CLUSTER to the Kurtosis cluster name to select}"
	configure_cluster
fi

annotate qrl.io/phase=running
qrltest run --report-dir "${report_dir}" "$@" "${lane}"
