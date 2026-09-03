#!/usr/bin/env bash
# Entrypoint of the qrl-tests-runner image when it runs as an in-cluster Job:
# point Kurtosis at the cluster it is running in, make sure the engine is up,
# run qrltest, and leave the verdict in the report directory. Outside a
# cluster (no service-account token) it only runs the command, for local use
# of the image against a Docker or pre-configured Kurtosis setup.
set -euo pipefail

command="${1:-${QRLTEST_COMMAND:?set QRLTEST_COMMAND or pass soak or an E2E lane}}"
shift $(( $# > 0 ? 1 : 0 ))

report_dir="${SOAK_REPORT_DIR:-${E2E_REPORT_DIR:-/reports}}"
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
	printf '{"exit_code":%d,"finished_at":"%s"}\n' "${status}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${report_dir}/job-status.json" || true
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
  - name: ${kurtosis_cluster}
    cluster:
      server: https://kubernetes.default.svc
      certificate-authority: ${service_account_dir}/ca.crt
users:
  - name: job
    user:
      tokenFile: ${service_account_dir}/token
contexts:
  - name: ${kurtosis_cluster}
    context:
      cluster: ${kurtosis_cluster}
      user: job
      namespace: $(cat "${service_account_dir}/namespace")
current-context: ${kurtosis_cluster}
EOF
	export KUBECONFIG="${kubeconfig}"

	local config_path data_dir selected
	config_path=$(kurtosis config path)
	# cluster-setting lives in the XDG data dir (~/.local/share/kurtosis),
	# not next to kurtosis-config.yml. Writing it avoids `cluster set`, which
	# tries to tear down the image default (docker) and then looks that name
	# up in a ConfigMap that only defines the EKS cluster.
	data_dir="${XDG_DATA_HOME:-${HOME}/.local/share}/kurtosis"
	mkdir -p "$(dirname "${config_path}")" "${data_dir}"
	printf '%s' "${kurtosis_cluster}" >"${data_dir}/cluster-setting"
	cp "${kurtosis_config_source}" "${config_path}"
	kurtosis analytics disable >/dev/null 2>&1 || true
	selected=$(kurtosis cluster get 2>/dev/null | awk 'NF{line=$0} END{print line}')
	if [ "${selected}" != "${kurtosis_cluster}" ]; then
		echo "Kurtosis cluster is '${selected:-unset}', expected ${kurtosis_cluster}" >&2
		return 1
	fi

	# Leftover engines from a failed soak answer the kube API but not the
	# server. Drop them so `engine start` creates a fresh one.
	kubectl get ns -o name 2>/dev/null | grep -E 'kurtosis-engine-|kt-qrl-soak-' | while read -r ns; do
		echo "removing leftover ${ns}"
		kubectl delete "${ns}" --wait=false || true
	done

	# `engine start` is idempotent when the existing engine answers. After a
	# failed soak the pod can be up while the server is not; restart then.
	annotate qrl.io/phase=engine
	if ! kurtosis engine start --version "${KURTOSIS_VERSION}" --log-retention-period "${engine_log_retention}"; then
		echo "engine start failed; restarting the leftover engine" >&2
		kurtosis engine restart --version "${KURTOSIS_VERSION}" --log-retention-period "${engine_log_retention}"
	fi

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

entrypoint_pid=$$
trap 'finish $?' EXIT
trap 'exit 143' TERM

if [ "${SOAK_CHAOS:-}" = "abort-midrun" ]; then
	(
		sleep "${SOAK_CHAOS_AFTER:-1200}"
		echo "chaos: abort-midrun after ${SOAK_CHAOS_AFTER:-1200}s" >&2
		kill -TERM "${entrypoint_pid}" 2>/dev/null || true
	) &
fi

annotate qrl.io/phase=starting

if [ -f "${service_account_dir}/token" ]; then
	: "${kurtosis_cluster:?set KURTOSIS_CLUSTER to the Kurtosis cluster name to select}"
	configure_cluster
fi

annotate qrl.io/phase=running
if [ "${command}" = soak ]; then
	qrltest soak --report-dir "${report_dir}" "$@"
else
	qrltest run --report-dir "${report_dir}" "$@" "${command}"
fi
