#!/usr/bin/env bash

# Sourced only by the disposable local-kind node-context verifier.
prepare_node_context_tls() {
  local cluster=$1 work_dir=$2 node=$3 ip=$4
  local owner
  owner=$(docker inspect --format '{{index .Config.Labels "io.x-k8s.kind.cluster"}}' "${node}")
  [ "${owner}" = "${cluster}" ] || { echo 'refusing TLS changes outside the owned kind cluster' >&2; return 1; }
  docker exec -i "${node}" sh -c 'umask 077; cat > /tmp/node-context-serving.cnf' <<EOF
[req]
distinguished_name = dn
prompt = no
[dn]
CN = ${node}
[serving]
basicConstraints = CA:FALSE
keyUsage = digitalSignature,keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = IP:${ip},DNS:${node}
EOF
  docker exec "${node}" sh -c '
    set -eu
    umask 077
    openssl req -new -newkey rsa:2048 -nodes -config /tmp/node-context-serving.cnf \
      -keyout /var/lib/kubelet/pki/node-context.key -out /tmp/node-context.csr
    openssl x509 -req -in /tmp/node-context.csr -CA /etc/kubernetes/pki/ca.crt \
      -CAkey /etc/kubernetes/pki/ca.key -set_serial 1001 -days 1 \
      -extfile /tmp/node-context-serving.cnf -extensions serving -out /var/lib/kubelet/pki/node-context.crt
    sed -i "/^tlsCertFile:/d; /^tlsPrivateKeyFile:/d" /var/lib/kubelet/config.yaml
    printf "\ntlsCertFile: /var/lib/kubelet/pki/node-context.crt\ntlsPrivateKeyFile: /var/lib/kubelet/pki/node-context.key\n" >> /var/lib/kubelet/config.yaml
    systemctl restart kubelet
  ' > "${work_dir}/serving-tls.log" 2>&1
  docker cp "${node}:/etc/kubernetes/pki/ca.crt" "${work_dir}/serving-ca.crt" >/dev/null
}

node_context_probe() {
  local work_dir=$1 namespace=$2 node=$3 image=$4 audience=$5 name=$6 account=$7 ca=$8 expected=$9
  local target=${10:-${node}}
  kctl apply -n "${namespace}" -f - >/dev/null <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: ${name}
spec:
  nodeName: ${node}
  serviceAccountName: ${account}
  automountServiceAccountToken: false
  restartPolicy: Never
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    seccompProfile: {type: RuntimeDefault}
  containers:
    - name: probe
      image: ${image}
      imagePullPolicy: Never
      args: ["--once", "--node-name=${target}", "--kubelet-ca=/trust/${ca}", "--kubelet-token-file=/var/run/secrets/kubernetes.io/serviceaccount/token"]
      resources:
        requests: {cpu: 10m, memory: 32Mi}
        limits: {memory: 64Mi}
      securityContext:
        allowPrivilegeEscalation: false
        readOnlyRootFilesystem: true
        capabilities: {drop: ["ALL"]}
      volumeMounts:
        - {name: trust, mountPath: /trust, readOnly: true}
        - {name: identity, mountPath: /var/run/secrets/kubernetes.io/serviceaccount, readOnly: true}
  volumes:
    - name: trust
      configMap: {name: node-context-trust}
    - name: identity
      projected:
        defaultMode: 0440
        sources:
          - serviceAccountToken: {path: token, expirationSeconds: 600, audience: "${audience}"}
          - configMap:
              name: kube-root-ca.crt
              items: [{key: ca.crt, path: ca.crt}]
EOF
  if ! kctl wait -n "${namespace}" "pod/${name}" --for="jsonpath={.status.phase}=${expected}" --timeout=90s > "${work_dir}/${name}-wait.log" 2>&1; then
    kctl logs -n "${namespace}" "${name}" > "${work_dir}/${name}.log" 2>&1 || true
    kctl get pod -n "${namespace}" "${name}" -o json > "${work_dir}/${name}-status.json"
    python3 - "${work_dir}/${name}-status.json" <<'PY'
import json, sys
p=json.load(open(sys.argv[1])); status=p.get('status', {})
states=[c.get('state', {}) for c in status.get('containerStatuses', [])]
print(json.dumps({'phase':status.get('phase'), 'containers':[
 {'state':kind, 'reason':value.get('reason'), 'exitCode':value.get('exitCode')}
 for state in states for kind,value in state.items()]}),file=sys.stderr)
PY
    echo "node-context ${name} did not reach ${expected}" >&2
    return 1
  fi
  kctl logs -n "${namespace}" "${name}" > "${work_dir}/${name}.log"
}
